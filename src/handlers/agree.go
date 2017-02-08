package handlers

// on click - start new subscription without any confirmation from telco side

import (
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	queue_config "github.com/vostrok/utils/config"
	rec "github.com/vostrok/utils/rec"
)

func HandlePull(c *gin.Context) {
	var r rec.Record
	var err error
	var msg rbmq.AccessCampaignNotify
	action := rbmq.UserActionsNotify{
		Action: "pull_click",
	}
	defer func() {
		if err != nil {
			m.Errors.Inc()

			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("handle pull")
		}
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid

		if err := notifierService.ActionNotify(action); err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("notify user action")
		} else {
		}
	}()

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		m.CampaignHashWrong.Inc()

		err := fmt.Errorf("Wrong campaign length: len %d, %s", len(campaignHash), campaignHash)
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	campaign, ok := campaignByHash[campaignHash]
	if !ok {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Cann't find campaign by hash: %s", campaignHash)
		return
	}
	msg = gatherInfo(c, campaign)
	if msg.Error != "" {
		return
	}
	if err = startNewSubscription(c, msg); err != nil {
		err = fmt.Errorf("startNewSubscription: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	m.Success.Inc()
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
