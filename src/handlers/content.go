package handlers

import (
	"errors"
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	content_service "github.com/vostrok/contentd/service"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/dispatcherd/src/utils"
	inmem_service "github.com/vostrok/inmem/service"
)

func AddContentHandlers(e *gin.Engine, rg *gin.RouterGroup) {
	e.GET("/u/:uniqueurl", AccessHandler, UniqueUrlGet)
	rg.GET("/contentget", AccessHandler, ContentGet)
}

// same as handle pull, but do not create subscription
// and has different metrics in the end
func ContentGet(c *gin.Context) {
	var err error

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
	contentProperties := &inmem_service.ContentSentProperties{}
	defer func() {
		if err != nil {
			m.Errors.Inc()
			c.Error(err)
			action.Error = err.Error()
			log.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("cann't process")
		}

		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		}
		if err = notifierService.ContentSentNotify(*contentProperties); err != nil {
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
				"data":  fmt.Sprintf("%#v", contentProperties),
			}).Info("notify content sent error")
		}
		sessions.RemoveTid(c)
	}()

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Wrong campaign length: len %d, %s", len(campaignHash), campaignHash)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	campaign, ok := campaignByHash[campaignHash]
	if !ok {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Cann't find campaign: %s", campaignHash)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	action.CampaignId = campaign.Id
	msg := gatherInfo(c, campaign)

	action.Msisdn = msg.Msisdn

	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties, err = content.Get(content_service.GetContentParams{
		Msisdn:     msg.Msisdn,
		Tid:        tid,
		CampaignId: campaign.Id,
		ServiceId:  campaign.ServiceId,
	})
	if err != nil {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.Fatal("contentd fatal: trying to free all resources")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	if contentProperties.ContentId == 0 {
		err = fmt.Errorf("content.Get: %s", "No content id")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	contentProperties.CampaignId = campaign.Id
	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("contentClient.Get: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("contentClient.Get")
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
		err = fmt.Errorf("serveContentFile: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
	m.ContentGetSuccess.Inc()
	m.Success.Inc()
}

func UniqueUrlGet(c *gin.Context) {

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
		Action: "content_get",
		Tid:    tid,
	}

	var err error
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		contentProperties.Tid = tid
		action.Tid = tid
		action.CampaignId = contentProperties.CampaignId

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

	if uniqueUrl == "get" {
		m.RandomContentGet.Inc()
		contentProperties, err = content.Get(content_service.GetContentParams{
			Msisdn:     sessions.GetFromSession("msisdn", c),
			Tid:        tid,
			ServiceId:  777,
			CampaignId: 290,
		})
	} else {
		m.UniqueUrlGet.Inc()
		contentProperties, err = content.GetByUniqueUrl(uniqueUrl)
	}
	if err != nil {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.GetByUniqueUrl: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("cannot get path by url")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		logCtx.Fatal("contentd fatal: trying to free all resources")
		return
	}
	if contentProperties.Error != "" || contentProperties.ContentId == 0 {
		m.ContentDeliveryErrors.Inc()

		if contentProperties.ContentId == 0 {
			contentProperties.Error = contentProperties.Error + " no uniq url found"
		}
		err = fmt.Errorf("content.GetByUniqueUrl: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("error while attemplting to get content")
		err = errors.New(contentProperties.Error)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	action.CampaignId = contentProperties.CampaignId
	action.Msisdn = contentProperties.Msisdn
	action.Tid = contentProperties.Tid
	action.Error = contentProperties.Error

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
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")

	m.Success.Inc()
}
