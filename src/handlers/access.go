package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
)

var e *gin.Engine

func AddCampaignHandler(e *gin.Engine, rg *gin.RouterGroup) {
	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)

	e.LoadHTMLGlob(cnf.Server.Path + "campaign/**/*")
	e.GET("/updateTemplates", updateTemplates)

	if cnf.Service.OnCliekNewSubscription {
		rg.GET("", AccessHandler, HandlePull)
	}
}

func updateTemplates(c *gin.Context) {
	path := cnf.Server.Path + "campaign/**/*"
	log.Debugf("update templates path: %s", path)
	e.LoadHTMLGlob(path)
	UpdateCampaigns()
	c.JSON(200, struct{}{})
}

func AccessHandler(c *gin.Context) {
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

func serveCampaigns(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
	}
	m.Incoming.Inc()

	var err error
	var msg rbmq.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("serve campaign")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("notify user action")
		}
		if errAccessCampaign := notifierService.AccessCampaignNotify(msg); errAccessCampaign != nil {
			logCtx.WithFields(log.Fields{
				"error": errAccessCampaign.Error(),
				"msg":   fmt.Sprintf("%#v", msg),
			}).Error("notify access campaign")
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
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	msg = gatherInfo(c, *campaign)
	if msg.IP == "" {
		m.IPNotFoundError.Inc()
	}
	if msg.Error == "Msisdn not found" {
		m.MsisdnNotFoundError.Inc()
	}
	if !msg.Supported {
		m.NotSupported.Inc()
	}

	if cnf.Service.Rejected.TrafficRedirectEnabled {
		// check if rejected: if rejected, then campaignId differs from campaign.id
		isRejected, err := inmem_client.IsMsisdnRejectedByService(msg.ServiceId, msg.Msisdn)
		if err != nil {
			err = fmt.Errorf("inmem_client.IsMsisdnRejectedByService: %s", err.Error())
			log.WithFields(log.Fields{
				"tid":   msg.Tid,
				"error": err.Error(),
			}).Error("rejected check failed")
		} else {
			if isRejected {
				trafficRedirect(msg, c)
				return
			} else {
				err := inmem_client.SetMsisdnServiceCache(msg.ServiceId, msg.Msisdn)
				if err != nil {
					err = fmt.Errorf("inmem_client.SetMsisdnServiceCache: %s", err.Error())
					log.WithFields(log.Fields{
						"tid":   msg.Tid,
						"error": err.Error(),
					}).Error("set msisdn service")
				}
			}
		}
	}

	if cnf.Service.RedirectOnGatherError && msg.Error != "" {
		log.WithFields(log.Fields{
			"err": msg.Error,
		}).Debug("gather info failed")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	campaignByLink[campaignLink].SimpleServe(c)

	m.CampaignAccess.Inc()
	m.Success.Inc()

	if campaignByLink[campaignLink].CanAutoClick {
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
				}).Info("error add new subscription")
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

		if err = startNewSubscription(c, msg); err == nil {
			log.WithFields(log.Fields{
				"tid":        msg.Tid,
				"link":       campaignLink,
				"hash":       campaignByLink[campaignLink].Hash,
				"msisdn":     msg.Msisdn,
				"campaignid": campaignByLink[campaignLink].Id,
			}).Info("added new subscritpion due to ratio")
		}
	}
	return
}
