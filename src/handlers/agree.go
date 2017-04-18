package handlers

// on click - start new subscription without any confirmation from telco side

import (
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	queue_config "github.com/linkit360/go-utils/config"
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

	service, err := inmem_client.GetServiceById(msg.ServiceId)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error":      err.Error(),
			"service_id": msg.ServiceId,
		}).Error("cannot get service by id")
		return err
	}
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
		DelayHours:         service.DelayHours,
		PaidHours:          service.PaidHours,
		KeepDays:           service.KeepDays,
		Price:              100 * int(service.Price),
	}
	if service.SendNotPaidTextEnabled {
		r.SMSSend = true
		r.SMSText = service.NotPaidText
	}

	operator, err := inmem_client.GetOperatorByCode(msg.OperatorCode)
	if err != nil {
		m.OperatorNameError.Inc()

		err = fmt.Errorf("inmem_client.GetOperatorByCode: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error": err.Error(),
			"code":  msg.OperatorCode,
		}).Error("cannot get operator by code")
		return err
	}
	queue := queue_config.NewSubscriptionQueueName(operator.Name)
	if err = notifierService.NewSubscriptionNotify(queue, r); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("notify new subscription")
		return err
	}
	m.AgreeSuccess.Inc()
	if cnf.Service.Rejected.CampaignRedirectEnabled {
		if err = inmem_client.SetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn); err != nil {
			err = fmt.Errorf("inmem_client.SetMsisdnCampaignCache: %s", err.Error())
			logCtx.Error(err.Error())
		}
	}
	return nil
}
