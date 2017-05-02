package handlers

// on click - start new subscription without any confirmation from telco side

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	rec "github.com/linkit360/go-utils/rec"
)

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
