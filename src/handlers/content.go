package handlers

import (
	"errors"
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content_client "github.com/linkit360/go-contentd/rpcclient"
	content_service "github.com/linkit360/go-contentd/server/src/service"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	"github.com/linkit360/go-dispatcherd/src/utils"
	inmem_service "github.com/linkit360/go-mid/service"
	"github.com/linkit360/go-utils/rec"
)

func AddContentHandlers() {
	e.GET("/u/:uniqueurl", AccessHandler, UniqueUrlGet)
	e.Group("/content/:campaign_hash").GET("", AccessHandler, ContentGet)
}

// gets the random content and sends it as a file
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
	action.CampaignCode = campaign.Code
	msg := gatherInfo(c, campaign)

	startNewSubscriptionFlag, _ := c.GetQuery("s")
	if len(startNewSubscriptionFlag) > 0 && msg.Msisdn != "" {
		if err = startNewSubscription(c, msg); err == nil {
			log.WithFields(log.Fields{
				"tid":           msg.Tid,
				"msisdn":        msg.Msisdn,
				"campaign_code": campaign.Code,
			}).Info("added new subscritpion")

			subAction := rbmq.UserActionsNotify{
				Action:       "pull_click",
				Tid:          tid,
				Msisdn:       msg.Msisdn,
				CampaignCode: campaign.Code,
			}
			if err := notifierService.ActionNotify(subAction); err != nil {
				logCtx.WithField("error", err.Error()).Error("notify user action")
			}
		}
	}

	action.Msisdn = msg.Msisdn

	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	operatorCode := int64(0)
	countryCode := int64(0)
	if cnf.Service.LandingPages.Mobilink.Enabled {
		operatorCode = cnf.Service.LandingPages.Mobilink.OperatorCode
		countryCode = cnf.Service.LandingPages.Mobilink.CountryCode
	} else {
		log.Error("content send: opcode/country code: not implemented for this telco")
	}

	contentProperties, err = content_client.Get(content_service.GetContentParams{
		Msisdn:       msg.Msisdn,
		Tid:          tid,
		CampaignCode: campaign.Code,
		ServiceCode:  campaign.ServiceCode,
		OperatorCode: operatorCode,
		CountryCode:  countryCode,
	})
	if err != nil {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.Fatal("contentd fatal: trying to free all resources")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	if contentProperties.ContentCode == "" {
		err = fmt.Errorf("content.Get: %s", "No content id")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	contentProperties.CampaignCode = campaign.Code
	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("contentClient.Get: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("contentClient.Get")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentCode,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

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

// unique link generated before (in mt, in dispatcher...)
// here user receives content by unique link
// if unique link was == "get" then we get
// unique content and send it, without unique link
func UniqueUrlGet(c *gin.Context) {

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	uniqueUrl := c.Params.ByName("uniqueurl")

	logCtx := log.WithFields(log.Fields{
		"tid": tid,
		"url": uniqueUrl,
	})
	logCtx.Debug("receive content by unique link")

	contentProperties := &inmem_service.ContentSentProperties{}
	action := rbmq.UserActionsNotify{
		Action: "content_get",
		Tid:    tid,
	}

	var err error
	defer func() {
		if err != nil {
			m.ContentDeliveryErrors.Inc()
			m.Errors.Inc()
			action.Error = err.Error()
		}
		contentProperties.Tid = tid
		action.Tid = tid
		action.CampaignCode = contentProperties.CampaignCode

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
		contentProperties, err = content_client.Get(content_service.GetContentParams{
			Msisdn:       sessions.GetFromSession("msisdn", c),
			Tid:          tid,
			ServiceCode:  "777",
			CampaignCode: "290",
		})
	} else {
		m.UniqueUrlGet.Inc()
		contentProperties, err = content_client.GetByUniqueUrl(uniqueUrl)
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
	if contentProperties.Error != "" || contentProperties.ContentCode == "" {
		m.ContentDeliveryErrors.Inc()

		if contentProperties.ContentCode == "" {
			contentProperties.Error = contentProperties.Error + " no uniq url found"
		}
		err = fmt.Errorf("content.GetByUniqueUrl: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("error while attemplting to get content")
		err = errors.New(contentProperties.Error)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentCode,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	action.CampaignCode = contentProperties.CampaignCode
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

	m.ContentGetSuccess.Inc()
}

// create unique url
func createUniqueUrl(r rec.Record) (contentUrl string, err error) {
	logCtx := log.WithFields(log.Fields{
		"tid": r.Tid,
	})

	contentProperties, err := content_client.GetUniqueUrl(content_service.GetContentParams{
		Msisdn:         r.Msisdn,
		Tid:            r.Tid,
		ServiceCode:    r.ServiceCode,
		CampaignCode:   r.CampaignCode,
		OperatorCode:   r.OperatorCode,
		CountryCode:    r.CountryCode,
		SubscriptionId: r.SubscriptionId,
	})

	if contentProperties.Error != "" {
		err = fmt.Errorf("contentProperties.Error: %s", contentProperties.Error)
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceCode,
			"error":     err.Error(),
		}).Error("contentd internal error")
		return
	}
	if err != nil {
		err = fmt.Errorf("content_client.GetUniqueUrl: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceCode,
			"error":     err.Error(),
		}).Error("cannot get unique content url")
		return
	}

	contentUrl = cnf.Server.Url + contentProperties.UniqueUrl
	return
}
