package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content_service "github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers/gather"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/dispatcherd/src/utils"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier
var contentSvc content_service.ContentService

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Notifier)
	content_service.InitService(conf.ContentService)
}

// uniq links generation ??
func HandlePull(c *gin.Context) {
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
	defer func(msg rbmq.AccessCampaignNotify, action rbmq.UserActionsNotify, err error) {
		if err != nil {
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		}
	}(msg, action, err)

	logCtx.Debug(c.Request.Header)

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Subscriptions.CampaignHashLength {
		logCtx.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")
		err := errors.New("Wrong campaign length")
		c.Error(err)
		msg.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("campaignHash", campaignHash)

	msg, err = gather.Gather(tid, campaignHash, c.Request)
	if err != nil {
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("msisdn", msg.Msisdn)
	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties, err := content_service.GetUrlByCampaignHash(
		content_service.GetUrlByCampaignHashParams{
			Msisdn:       msg.Msisdn,
			Tid:          tid,
			CampaignHash: campaignHash,
			CountryCode:  msg.CountryCode,
			OperatorCode: msg.OperatorCode,
		})
	if err != nil {
		err := fmt.Errorf("contentClient.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		msg.Error = err.Error()
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentPropertities": contentProperties,
	}).Debug("got content id, serving file")

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
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		msg.Error = err.Error()
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
}

func AddCampaignHandlers(r *gin.Engine) {
	for _, v := range campaigns.Get().Map {
		log.WithField("route", v.Link).Info("adding route")
		rg := r.Group("/" + v.Link)
		rg.Use(sessions.AddSessionTidHandler)
		rg.Use(NotifyAccessCampaignHandler)
		rg.GET("", v.Serve)
	}
}

func NotifyAccessCampaignHandler(c *gin.Context) {

	tid := sessions.GetTid(c)

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]
	campaign, ok := campaigns.Get().Map[campaignLink]
	campaignHash := ""
	if !ok {
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
	logCtx.Info("notify user action")
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
		}).Info("done notify user action")
	}

	msg, err := gather.Gather(tid, campaign.Hash, c.Request)
	if err != nil {
		logCtx.WithFields(log.Fields{
			"error":          err.Error(),
			"accessCampaign": msg,
		}).Error("gather access campaign error")
	}
	if err := notifierService.AccessCampaignNotify(msg); err != nil {
		logCtx.WithField("error", err.Error()).Error("notify access campaign")
	} else {
		logCtx.WithFields(log.Fields{
			"accessCampaign": msg,
		}).Info("done notify access campaign")
	}

}
