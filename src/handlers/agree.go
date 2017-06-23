package handlers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	mid_client "github.com/linkit360/go-mid/rpcclient"
	rec "github.com/linkit360/go-utils/rec"
	"github.com/linkit360/go-utils/structs"
)

// on click - start new subscription API for south team
// ALTER TABLE public.xmp_subscriptions ADD channel VARCHAR(255) DEFAULT '' NOT NULL;
func initiateSubscription(c *gin.Context) {
	var err error
	m.Incoming.Inc()

	msg := gatherInfo(c)

	logCtx := log.WithFields(log.Fields{
		"tid": msg.Tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "api_subscribe",
	}
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
			"link":  campaignLink,
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		c.JSON(500, gin.H{"error": "Unknown campaign"})
		return
	}
	msg.CampaignId = campaign.Id
	msg.ServiceCode = campaign.ServiceCode
	msg.CampaignHash = campaign.Hash
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
				"tid":         msg.Tid,
				"link":        campaignLink,
				"hash":        campaignByLink[campaignLink].Hash,
				"msisdn":      msg.Msisdn,
				"campaign_id": campaignByLink[campaignLink].Id,
			}).Info("added new subscritpion by API call")
			m.Success.Inc()
			c.JSON(200, gin.H{"state": "success"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	if "unsub" == qEvent || "purge" == qEvent || "unreg" == qEvent {
		if qEvent == "unsub" {
			qEvent = "unreg"
		}
		action.Action = qEvent
		r := rec.Record{
			Msisdn:       msg.Msisdn,
			ServiceCode:  msg.ServiceCode,
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
			"tid":         msg.Tid,
			"link":        campaignLink,
			"hash":        campaignByLink[campaignLink].Hash,
			"msisdn":      msg.Msisdn,
			"campaign_id": campaignByLink[campaignLink].Id,
		}).Info("added new subscritpion by API call (event unrecognized)")
	}
	c.JSON(500, gin.H{"error": "event is unrecognized"})
	return
}

func startNewSubscription(c *gin.Context, msg structs.AccessCampaignNotify) error {

	if cnf.Service.Rejected.CampaignRedirectEnabled {
		campaignRedirect, err := redirect(msg)
		if err != nil {
			return err
		}
		if campaignRedirect.Id == "" {
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
			msg.ServiceCode = campaignRedirect.ServiceCode
		}
	}

	if cnf.Service.Rejected.TrafficRedirectEnabled {
		err := mid_client.SetMsisdnServiceCache(msg.ServiceCode, msg.Msisdn)
		if err != nil {
			err = fmt.Errorf("mid_client.SetMsisdnServiceCache: %s", err.Error())
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

	logCtx.WithField("campaign", msg.CampaignId).Debug("start new subscription...")

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
		ServiceCode:        msg.ServiceCode,
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
		if err := mid_client.SetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn); err != nil {
			err = fmt.Errorf("mid_client.SetMsisdnCampaignCache: %s", err.Error())
			logCtx.Error(err.Error())
		}
	}
	return nil
}
