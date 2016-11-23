package handlers

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
	queue_config "github.com/vostrok/utils/config"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Notifier)

	content.Init(conf.ContentClient)
	inmem_client.Init(conf.InMemConfig)
}

func HandlePull(c *gin.Context) {
	m.Agree.Inc()

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	if tid == "" {
		tid = "testtid"
	}
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	var msg rbmq.AccessCampaignNotify
	action := rbmq.UserActionsNotify{
		Action: "pull_click",
		Tid:    tid,
	}
	var err error
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		} else {
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
		msg.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("campaignHash", campaignHash)

	msg, err = gatherInfo(tid, campaignHash, c)
	if err != nil {
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("msisdn", msg.Msisdn)
	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties := &content_service.ContentSentProperties{}
	contentProperties, err = content.Get(content_service.GetUrlByCampaignHashParams{
		Msisdn:       msg.Msisdn,
		Tid:          tid,
		CampaignHash: campaignHash,
		CountryCode:  msg.CountryCode,
		OperatorCode: msg.OperatorCode,
		Publisher:    sessions.GetFromSession("publisher", c),
		Pixel:        sessions.GetFromSession("pixel", c),
	})
	if err != nil {
		m.ContentdRPCDialError.Inc()
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		msg.Error = err.Error()
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
		msg.Error = contentProperties.Error
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	msg.CampaignId = contentProperties.CampaignId
	msg.ContentId = contentProperties.ContentId
	msg.ServiceId = contentProperties.ServiceId

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
		msg.Error = err.Error()
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
	m.AgreeSuccess.Inc()

	operator, err := inmem_client.GetOperatorByCode(msg.OperatorCode)
	if err != nil {
		logCtx.WithFields(log.Fields{
			"error":         err.Error(),
			"operator_code": msg.OperatorCode,
		}).Error("cannot get operator")
		m.OperatorNameError.Inc()
		return
	}
	queue := queue_config.GetNewSubscriptionQueueName(operator.Name)
	logCtx.WithField("queue", queue).Debug("inform new subscritpion")
	if err = notifierService.NewSubscriptionNotify(queue, contentProperties); err != nil {
		logCtx.WithField("error", err.Error()).Error("notify new subscription")
		return
	}
	m.Success.Inc()
}

func ContentGet(c *gin.Context) {
	m.CampaignAccess.Inc()

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	if tid == "" {
		tid = "testtid"
	}
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	var msg rbmq.AccessCampaignNotify
	action := rbmq.UserActionsNotify{
		Action: "content_get",
		Tid:    tid,
	}
	var err error
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		} else {
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
		msg.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("campaignHash", campaignHash)

	msg, err = gatherInfo(tid, campaignHash, c)
	if err != nil {
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("msisdn", msg.Msisdn)
	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties := &content_service.ContentSentProperties{}
	contentProperties, err = content.Get(content_service.GetUrlByCampaignHashParams{
		Msisdn:       msg.Msisdn,
		Tid:          tid,
		CampaignHash: campaignHash,
		CountryCode:  msg.CountryCode,
		OperatorCode: msg.OperatorCode,
		Publisher:    sessions.GetFromSession("publisher", c),
		Pixel:        sessions.GetFromSession("pixel", c),
	})
	if err != nil {
		m.ContentdRPCDialError.Inc()
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		msg.Error = err.Error()
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
		msg.Error = contentProperties.Error
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	msg.CampaignId = contentProperties.CampaignId
	msg.ContentId = contentProperties.ContentId
	msg.ServiceId = contentProperties.ServiceId

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
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
	m.ContentGetSuccess.Inc()
	m.Success.Inc()
}

// backward compatibility
func AddCampaignHandlers(r *gin.Engine) {
	campaigns, err := inmem_client.GetAllCampaigns()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot ad campaign handlers")
		return
	}
	for _, v := range campaigns {
		log.WithField("route", v.Link).Info("adding route")
		rg := r.Group("/" + v.Link)
		rg.Use(AccessHandler)
		rg.Use(NotifyAccessCampaignHandler)
		rg.GET("", v.Serve)
	}
}

// further
func AddCampaignHandler(r *gin.Engine) {
	log.WithField("route", "lp").Info("adding lp route")
	rg := r.Group("/lp/:campaign_link")
	rg.Use(AccessHandler)
	rg.Use(NotifyAccessCampaignHandler)
	rg.GET("", serveCampaigns)
}

func serveCampaigns(c *gin.Context) {
	m.Access.Inc()
	campaignLink := c.Params.ByName("campaign_link")
	campaign, err := inmem_client.GetCampaignByLink(campaignLink)
	if err != nil {
		m.PageNotFoundError.Inc()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
	}
	m.CampaignAccess.Inc()
	m.Success.Inc()
	utils.ServeBytes(campaign.Content, c)

}

// on each access page
func NotifyAccessCampaignHandler(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]
	campaign, err := inmem_client.GetCampaignByLink(campaignLink)
	campaignHash := ""
	if err != nil {
		log.WithFields(log.Fields{
			"error": "unknown campaign",
			"path":  campaignLink,
		}).Error("campaign is unknown")
	} else {
		campaignHash = campaign.Hash
	}
	logCtx := log.WithFields(log.Fields{
		"tid":          tid,
		"campaignHash": campaignHash,
	})

	begin := time.Now()
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
	}

	if err := notifierService.ActionNotify(action); err != nil {
		logCtx.WithFields(log.Fields{
			"error":  err.Error(),
			"action": action,
		}).Error("error notify user action")
	} else {
		logCtx.WithFields(log.Fields{
			"action": action,
			"took":   time.Since(begin),
		}).Info("done notify user action")
	}

	msg, err := gatherInfo(tid, campaign.Hash, c)
	if err != nil {
		logCtx.WithFields(log.Fields{
			"error":          err.Error(),
			"accessCampaign": msg,
		}).Error("gather access campaign error")
	}
	if err := notifierService.AccessCampaignNotify(msg); err != nil {
		logCtx.WithField("error", err.Error()).Error("notify access campaign")
	} else {
		logCtx.WithFields(log.Fields{}).Info("done notify access campaign")
	}
}

func AccessHandler(c *gin.Context) {
	m.Overall.Inc()
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
