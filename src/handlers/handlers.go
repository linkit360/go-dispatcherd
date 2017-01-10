package handlers

// handlers of dispatcher:
// handle pull
// handle get content
// handle serve campaigns
// access middleware
import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	content_service "github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/config"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/dispatcherd/src/utils"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
	queue_config "github.com/vostrok/utils/config"
	"github.com/vostrok/utils/rec"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier

var campaignByLink map[string]*inmem_service.Campaign
var campaignByHash map[string]inmem_service.Campaign

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf

	content.Init(conf.ContentClient)
	if err := inmem_client.Init(conf.InMemConfig); err != nil {
		log.Fatal("cannot init inmem client")
	}
	UpdateCampaigns()
	notifierService = rbmq.NewNotifierService(conf.Notifier)
}

func UpdateCampaigns() error {
	log.WithFields(log.Fields{}).Debug("get all campaigns")
	campaigns, err := inmem_client.GetAllCampaigns()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot add campaign handlers")
		return err
	}

	for key, _ := range campaignByLink {
		delete(campaignByLink, key)
	}
	for key, _ := range campaignByHash {
		delete(campaignByHash, key)
	}
	campaignByLink = make(map[string]*inmem_service.Campaign, len(campaigns))
	campaignByHash = make(map[string]inmem_service.Campaign, len(campaigns))
	for _, campaign := range campaigns {
		camp := campaign
		campaignByLink[campaign.Link] = &camp
		campaignByHash[campaign.Hash] = camp
	}
	log.WithFields(log.Fields{
		"len": len(campaigns),
		"c":   fmt.Sprintf("%#v", campaignByLink),
	}).Info("campaigns updated")
	return nil
}

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

		log.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")

		err := errors.New("Wrong campaign length")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	campaign, ok := campaignByHash[campaignHash]
	if !ok {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Cann't find campaign: %s", campaignHash)
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cann't process")
		return
	}
	msg, err = gatherInfo(c, campaign)
	if err != nil {
		return
	}
	campaignRedirect, err := redirect(action, msg)
	if err != nil {
		action.Action = "rejected"
		msg.Error = "rejected"
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	if campaignRedirect.Id != msg.CampaignId && campaignRedirect.Hash != "" {
		m.Redirected.Inc()
		action.Action = "redirect"
		msg.CampaignId = campaignRedirect.Id
		msg.ServiceId = campaignRedirect.ServiceId
		msg.CampaignHash = campaignRedirect.Hash
		msg.Error = "redirect"
	}
	if err = startNewSubscription(c, msg); err != nil {
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
}

func startNewSubscription(c *gin.Context, msg rbmq.AccessCampaignNotify) (err error) {
	m.Agree.Inc()
	logCtx := log.WithFields(log.Fields{
		"tid": msg.Tid,
	})

	logCtx.Debug("start new subscription")

	defer func() {
		sessions.RemoveTid(c)
	}()

	logCtx = logCtx.WithField("msisdn", msg.Msisdn)

	service, err := inmem_client.GetServiceById(msg.ServiceId)
	if err != nil {
		m.ServiceError.Inc()

		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error":      err.Error(),
			"service_id": msg.ServiceId,
		}).Error("cannot get service by id")
		return
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
		return
	}
	queue := queue_config.NewSubscriptionQueueName(operator.Name)
	logCtx.WithField("queue", queue).Debug("inform new subscritpion")
	if err = notifierService.NewSubscriptionNotify(queue, r); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("notify new subscription")
		return
	}

	m.AgreeSuccess.Inc()
	m.Success.Inc()

	if cnf.Service.Rejected.Enabled {
		if err = inmem_client.SetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn); err != nil {
			err = fmt.Errorf("inmem_client.SetMsisdnCampaignCache: %s", err.Error())
			log.Error(err.Error())
		}
	}
	return
}

// same as handle pull, but do not create subscription
// and has different metrics in the end
func ContentGet(c *gin.Context) {
	m.CampaignAccess.Inc()

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	logCtx.Debug("get content")

	action := rbmq.UserActionsNotify{
		Action: "content_get",
		Tid:    tid,
	}
	var err error
	contentProperties := &inmem_service.ContentSentProperties{}
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		} else {
		}
		if err = notifierService.ContentSentNotify(*contentProperties); err != nil {
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Info("notify content sent error")
		} else {
			logCtx.Info("notified")
		}
		sessions.RemoveTid(c)
	}()
	logCtx.Debug(c.Request.Header)

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		m.CampaignHashWrong.Inc()

		logCtx.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")

		err := errors.New("Wrong campaign length")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	campaign, ok := campaignByHash[campaignHash]
	if !ok {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Cann't find campaign: %s", campaignHash)
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cann't process")
		return
	}
	action.CampaignId = campaign.Id
	msg, err := gatherInfo(c, campaign)
	if err != nil {
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignHash = campaignHash
	action.Msisdn = msg.Msisdn
	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties, err = content.Get(content_service.GetContentParams{
		Msisdn:     msg.Msisdn,
		Tid:        tid,
		CampaignId: campaign.Id,
		ServiceId:  campaign.ServiceId,
	})
	contentProperties.CampaignId = campaign.Id
	if err != nil {
		m.ContentdRPCDialError.Inc()
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		logCtx.Fatal("contentd fatal: trying to free all resources")
		return
	}
	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("contentClient.Get: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("contentClient.Get")
		err = errors.New(contentProperties.Error)
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	// todo one time url-s
	err = utils.ServeAttachment(
		cnf.Server.Path+"uploaded_content/"+contentProperties.ContentPath,
		contentProperties.ContentName,
		c,
		logCtx,
	)
	if err != nil {
		m.ContentDeliveryErrors.Inc()
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
	m.ContentGetSuccess.Inc()
	m.Success.Inc()
}

func UniqueUrlGet(c *gin.Context) {
	m.CampaignAccess.Inc()

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	uniqueUrl := c.Params.ByName("uniqueurl")

	logCtx := log.WithFields(log.Fields{
		"tid": tid,
		"url": uniqueUrl,
	})
	logCtx.Debug("get unique url")

	contentProperties := &inmem_service.ContentSentProperties{}
	action := rbmq.UserActionsNotify{
		Action: "unique_url_open",
		Tid:    tid,
	}

	var err error
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		action.Tid = contentProperties.Tid

		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		}
		if err = notifierService.ContentSentNotify(*contentProperties); err != nil {
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("notify content sent error")
		}
		sessions.RemoveTid(c)
	}()
	logCtx.Debug(c.Request.Header)

	contentProperties, err = content.GetByUniqueUrl(uniqueUrl)
	if err != nil {
		m.ContentdRPCDialError.Inc()
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.GetByUniqueUrl: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("cannot get path by url")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		logCtx.Fatal("contentd fatal: trying to free all resources")
		return
	}
	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.GetByUniqueUrl: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("error while attemplting to get content")
		err = errors.New(contentProperties.Error)
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	action.CampaignId = contentProperties.CampaignId

	err = utils.ServeAttachment(
		cnf.Server.Path+"uploaded_content/"+contentProperties.ContentPath,
		contentProperties.ContentName,
		c,
		logCtx,
	)
	if err != nil {
		m.ContentDeliveryErrors.Inc()
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")

	m.Success.Inc()
}

func AddCampaignHandler(r *gin.Engine) {
	log.WithField("route", "lp").Info("adding lp route")
	rg := r.Group("/lp/:campaign_link")
	rg.Use(AccessHandler)
	rg.GET("", serveCampaigns)
}

func serveCampaigns(c *gin.Context) {
	begin := time.Now()
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
	}
	var err error
	var msg rbmq.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			action.Error = err.Error()
			msg.Error = err.Error()
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("access")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": action,
			}).Error("notify user action")
		} else {
			logCtx.WithFields(log.Fields{
				"action": action,
				"took":   time.Since(begin),
			}).Info("notify user action")
		}
		if errAccessCampaign := notifierService.AccessCampaignNotify(msg); errAccessCampaign != nil {
			logCtx.WithField("error", errAccessCampaign.Error()).Error("notify access campaign")
		} else {
			logCtx.WithFields(log.Fields{}).Info("notify access campaign")
		}
		if err != nil {
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		}
	}()

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]

	// important, do not use campaign from this operation
	// bcz we need to inc counter to process ratio
	if _, ok := campaignByLink[campaignLink]; !ok {
		m.PageNotFoundError.Inc()

		log.WithFields(log.Fields{
			"campaignLink": campaignLink,
			"error":        "not found",
		}).Error("cannot get campaign by link")
		err = fmt.Errorf("page not found: %s", campaignLink)
		return
	}
	campaign, _ := campaignByLink[campaignLink]
	msg, err = gatherInfo(c, *campaign)
	if err != nil {
		return
	}
	campaignRedirect, err := redirect(action, msg)
	if err != nil {
		return
	}
	if campaignRedirect.Id == 0 {
		action.Action = "access"
		msg.Error = "rejected"
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	if campaignRedirect.Id != msg.CampaignId && campaignRedirect.Hash != "" {
		m.Redirected.Inc()

		action.Action = "redirect"
		msg.Error = "redirect"
		url := cnf.Service.Rejected.RedirectUrl + campaignRedirect.Hash
		http.Redirect(c.Writer, c.Request, url, 303)
		return
	} else {
		campaignByLink[campaignLink].Serve(c)
	}

	m.CampaignAccess.Inc()
	m.Success.Inc()

	if campaignByLink[campaignLink].CanAutoClick {
		var err error
		var msg rbmq.AccessCampaignNotify

		// todo: refactor in handle pull
		action := rbmq.UserActionsNotify{
			Action: "autoclick",
		}
		defer func() {
			if err != nil {
				m.Errors.Inc()
				action.Error = err.Error()
				log.WithFields(log.Fields{
					"tid":    msg.Tid,
					"msisdn": msg.Msisdn,
					"link":   campaignLink,
					"error":  err.Error(),
				}).Info("error serve campaign")
			}
			action.Tid = msg.Tid
			action.Msisdn = msg.Msisdn
			action.CampaignId = msg.CampaignId

			if err := notifierService.ActionNotify(action); err != nil {
				log.WithFields(log.Fields{
					"error": err.Error(),
					"tid":   msg.Tid,
				}).Error("notify user action")
			} else {
			}
		}()

		err = startNewSubscription(c, msg)
		if err == nil {
			log.WithFields(log.Fields{
				"tid":        msg.Tid,
				"link":       campaignLink,
				"hash":       campaignByLink[campaignLink].Hash,
				"msisdn":     msg.Msisdn,
				"campaignid": campaignByLink[campaignLink].Id,
			}).Info("added new subscritpion due to ratio")
		}
	}
}

func redirect(action rbmq.UserActionsNotify, msg rbmq.AccessCampaignNotify) (
	campaign inmem_service.Campaign, err error) {
	if !cnf.Service.Rejected.Enabled {
		log.WithFields(log.Fields{
			"tid": msg.Tid,
		}).Debug("redirect off")
		campaign.Id = msg.CampaignId
		return
	}

	// if nextCampaignId == msg.CampaignId then it's not rejected msisdn
	campaign.Id, err = inmem_client.GetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetMsisdnCampaignCache: %s", err.Error())
		log.WithFields(log.Fields{
			"tid":       msg.Tid,
			"msisdn":    msg.Msisdn,
			"campaign":  msg.CampaignId,
			"2campaign": campaign.Id,
			"error":     err.Error(),
		}).Debug("redirect check faieled")
		return
	}

	if campaign.Id == msg.CampaignId {
		log.WithFields(log.Fields{
			"tid":       msg.Tid,
			"msisdn":    msg.Msisdn,
			"campaign":  msg.CampaignId,
			"2campaign": campaign.Id,
		}).Debug("no redirect: ok")
		return
	}
	// no more campaigns
	if campaign.Id == 0 {
		m.NoMoreCampaigns.Inc()

		action.Error = "No more campaigns"
		log.WithFields(log.Fields{
			"tid":       msg.Tid,
			"msisdn":    msg.Msisdn,
			"campaign":  msg.CampaignId,
			"2campaign": campaign.Id,
			"error":     action.Error,
		}).Debug("redirect")
		return
	}

	campaign, err = inmem_client.GetCampaignById(msg.CampaignId)
	if err != nil {
		log.WithFields(log.Fields{
			"tid":       msg.Tid,
			"msisdn":    msg.Msisdn,
			"campaign":  msg.CampaignId,
			"2campaign": campaign.Id,
			"error":     err.Error(),
		}).Debug("redirect")
		err = fmt.Errorf("inmem_client.GetCampaignById: %s", err.Error())
		return
	}
	log.WithFields(log.Fields{
		"tid":       msg.Tid,
		"msisdn":    msg.Msisdn,
		"campaign":  msg.CampaignId,
		"2campaign": campaign.Id,
	}).Debug("redirect")
	return
}

// just log and count all requests
func AccessHandler(c *gin.Context) {
	m.Overall.Inc()
	m.Access.Inc()

	begin := time.Now()
	c.Next()

	responseTime := time.Since(begin)
	tid := sessions.GetTid(c)

	if len(c.Errors) > 0 {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"error":  c.Errors.String(),
			"since":  responseTime,
		}).Error(c.Errors.String())
	} else {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"since":  responseTime,
		}).Info("access")
	}
	c.Header("X-Response-Time", responseTime.String())
}
