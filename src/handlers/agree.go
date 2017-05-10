package handlers

import (
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	rec "github.com/linkit360/go-utils/rec"
)

// on click - start new subscription API for south team
// ALTER TABLE public.xmp_subscriptions ADD channel VARCHAR(255) DEFAULT '' NOT NULL;
func initiateSubscription(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "direct_start",
		Tid:    tid,
	}
	m.Incoming.Inc()

	var err error
	var msg rbmq.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("subscribe")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("notify user action")
		}
	}()

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]

	// important, do not use campaign from this operation
	// bcz we need to inc counter to process ratio
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		c.JSON(500, gin.H{"error": "Unknown campaign"})
		return
	}

	msg = gatherInfo(c, *campaign)
	if msg.IP == "" {
		m.IPNotFoundError.Inc()
	}
	if msg.Error == "Msisdn not found" {
		m.MsisdnNotFoundError.Inc()

		log.WithFields(log.Fields{
			"error": msg.Error,
		}).Error("msisdn required")
		c.JSON(500, gin.H{"error": "msisdn required"})
		return
	}

	if !msg.Supported {
		m.NotSupported.Inc()

		err = fmt.Errorf("Operator not recognized: %s", msg.Msisdn)
		log.WithFields(log.Fields{
			"error": msg.Error,
		}).Error("cann't process")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	if msg.Error != "" {
		log.WithFields(log.Fields{
			"err": msg.Error,
		}).Error("gather info failed")
		c.JSON(500, gin.H{"error": msg.Error})
		return
	}

	// i.e. 923009102250&channel=slypee&event=sub
	qEvent := c.DefaultQuery("event", "")
	if "sub" == qEvent {
		if err = startNewSubscription(c, msg); err == nil {
			log.WithFields(log.Fields{
				"tid":        msg.Tid,
				"link":       campaignLink,
				"hash":       campaignByLink[campaignLink].Hash,
				"msisdn":     msg.Msisdn,
				"campaignid": campaignByLink[campaignLink].Id,
			}).Info("added new subscritpion by API call")
			m.Success.Inc()
			c.JSON(200, gin.H{"state": "success"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	if "unsub" == qEvent || "purge" == qEvent {
		r := rec.Record{
			Msisdn:       msg.Msisdn,
			ServiceId:    msg.ServiceId,
			CampaignId:   msg.CampaignId,
			Tid:          msg.Tid,
			CountryCode:  msg.CountryCode,
			OperatorCode: msg.OperatorCode,
			Channel:      c.DefaultQuery("channel", ""),
		}

		if err := notifierService.Notify(
			cnf.Service.LandingPages.Mobilink.Queues.Responses, qEvent, r); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		m.Success.Inc()
		c.JSON(200, gin.H{"state": "success"})
		return
	}
	if err = startNewSubscription(c, msg); err == nil {
		log.WithFields(log.Fields{
			"tid":        msg.Tid,
			"link":       campaignLink,
			"hash":       campaignByLink[campaignLink].Hash,
			"msisdn":     msg.Msisdn,
			"campaignid": campaignByLink[campaignLink].Id,
		}).Info("added new subscritpion by API call (event unrecognized)")
	}
	c.JSON(500, gin.H{"error": "event is unrecognized"})
	return
}

func startNewSubscription(c *gin.Context, msg rbmq.AccessCampaignNotify) error {
	if cnf.Service.Rejected.CampaignRedirectEnabled {
		campaignRedirect, err := redirect(msg)
		if err != nil {
			return err
		}
		if campaignRedirect.Id == 0 {
			msg.Error = "rejected"
			log.WithFields(log.Fields{
				"url": cnf.Service.ErrorRedirectUrl,
			}).Debug("rejected")
		} else if campaignRedirect.Id != msg.CampaignId {
			m.Redirected.Inc()

			log.WithFields(log.Fields{
				"tid": msg.Tid,
			}).Info("redirect")

			msg.CampaignHash = campaignRedirect.Hash
			msg.CampaignId = campaignRedirect.Id
			msg.ServiceId = campaignRedirect.ServiceId
		}
	}
	if cnf.Service.Rejected.TrafficRedirectEnabled {
		err := inmem_client.SetMsisdnServiceCache(msg.ServiceId, msg.Msisdn)
		if err != nil {
			err = fmt.Errorf("inmem_client.SetMsisdnServiceCache: %s", err.Error())
			log.WithFields(log.Fields{
				"tid":   msg.Tid,
				"error": err.Error(),
			}).Error("set msisdn service")
		}
	}

	m.Agree.Inc()
	logCtx := log.WithFields(log.Fields{
		"tid": msg.Tid,
	})
	logCtx.WithField("campaign", msg.CampaignId).Debug("start new subscription")

	defer func() {
		sessions.RemoveTid(c)
	}()

	logCtx = logCtx.WithField("msisdn", msg.Msisdn)

	r := rec.Record{
		Msisdn:             msg.Msisdn,
		Tid:                msg.Tid,
		SubscriptionStatus: "",
		CountryCode:        msg.CountryCode,
		OperatorCode:       msg.OperatorCode,
		Publisher:          sessions.GetFromSession("publisher", c),
		Pixel:              sessions.GetFromSession("pixel", c),
		CampaignId:         msg.CampaignId,
		ServiceId:          msg.ServiceId,
		Channel:            c.DefaultQuery("channel", ""),
	}

	var moQueue string
	if cnf.Service.LandingPages.Mobilink.Enabled {
		moQueue = cnf.Service.LandingPages.Mobilink.Queues.MO
	} else {
		log.Fatal("not implemented for this telco")
	}
	if err := notifierService.NewSubscriptionNotify(moQueue, r); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("notify new subscription")
		return err
	}
	m.AgreeSuccess.Inc()
	if cnf.Service.Rejected.CampaignRedirectEnabled {
		if err := inmem_client.SetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn); err != nil {
			err = fmt.Errorf("inmem_client.SetMsisdnCampaignCache: %s", err.Error())
			logCtx.Error(err.Error())
		}
	}
	return nil
}
