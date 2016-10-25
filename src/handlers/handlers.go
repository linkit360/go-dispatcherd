package handlers

import (
	"errors"
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers/gather"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/dispatcherd/src/utils"
	"strings"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier
var contentClient *content.Client

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Notifier)

	var err error
	contentClient, err = content.NewClient(conf.ContentClient.DSN, conf.ContentClient.Timeout)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Init content service rpc client")
	}
}

// uniq links generation ??
func HandlePull(c *gin.Context) {
	tid := sessions.GetTid(c)
	logCtx := log.WithField("tid", tid)

	var msg rbmq.AccessCampaignNotify
	action := rbmq.UserActionNotify{
		Action: "pull_click",
		Tid:    tid,
	}
	var err error
	defer func(msg rbmq.AccessCampaignNotify, action rbmq.UserActionNotify, err error) {
		action.Error = err
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		}
	}(msg, action, err)

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Subscriptions.CampaignHashLength {
		logCtx.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")
		err := errors.New("Wrong campaign length")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx = logCtx.WithField("campaignHash", campaignHash)

	msg, err = gather.Gather(tid, campaignHash, c.Request)
	if err != nil {
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	contentProperties, err := contentClient.Get(service.GetUrlByCampaignHashParams{
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
		msg.Error = err
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignId = contentProperties.CampaignId
	msg.ContentId = contentProperties.ContentId
	msg.ServiceId = contentProperties.ServiceId

	// todo one time url-s
	if err = utils.ServeFile(cnf.Server.StaticPath+contentProperties.ContentPath, c); err != nil {
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		msg.Error = err
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
	}
}

func AddCampaignHandlers(r *gin.Engine) {
	for _, v := range campaigns.Get().Map {
		log.WithField("route", v.Link).Info("adding route")
		rg := r.Group("/" + v.Link)
		rg.Use(NotifyOpenHandler)
		rg.GET("", v.Serve)
	}
}

func NotifyOpenHandler(c *gin.Context) {
	tid := sessions.GetTid(c)
	paths := strings.Split(c.Request.URL.Path, "/")
	campaignHash := paths[len(paths)-1]

	logCtx := log.WithFields(log.Fields{
		"tid":          tid,
		"campaignHash": campaignHash,
	})
	logCtx.Info("new access")

	action := rbmq.UserActionNotify{
		Action: "access",
		Tid:    tid,
	}

	if err := notifierService.ActionNotify(action); err != nil {
		logCtx.WithFields(log.Fields{
			"error":  err.Error(),
			"action": action,
		}).Error("notify user action")
	}
	msg, err := gather.Gather(tid, campaignHash, c.Request)
	if err != nil {
		logCtx.WithFields(log.Fields{
			"error":          err.Error(),
			"accessCampaign": msg,
		}).Error("gather access campaign error")
	}
	if err := notifierService.AccessCampaignNotify(msg); err != nil {
		logCtx.WithField("error", err.Error()).Error("notify access campaign")
	}

}
