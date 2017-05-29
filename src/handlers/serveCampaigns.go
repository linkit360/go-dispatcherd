package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-mid/rpcclient"
	"github.com/linkit360/go-utils/rec"
	"github.com/linkit360/go-utils/structs"
)

func AddCampaignHandler(rg *gin.RouterGroup) {
	if !cnf.Service.LandingPages.Custom {
		e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	}
	e.LoadHTMLGlob(cnf.Server.Path + "campaign/**/*")
	e.GET("/updateTemplates", updateTemplates)
}

func updateTemplates(c *gin.Context) {
	path := cnf.Server.Path + "campaign/**/*"
	log.Debugf("update templates path: %s", path)
	e.LoadHTMLGlob(path)
	UpdateCampaigns()
	c.JSON(200, struct{}{})
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
	var msg structs.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignCode = msg.CampaignCode
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
		// check if rejected: if rejected, then campaignCode differs from campaign.id
		isRejected, err := inmem_client.IsMsisdnRejectedByService(msg.ServiceCode, msg.Msisdn)
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

	if cnf.Service.SendRestorePixelEnabled {
		val, ok := c.GetQuery("aff_sub")
		if ok && len(val) >= 5 {
			log.WithFields(log.Fields{
				"tid": tid,
			}).Debug("found pixel in get params")
			if err := notifierService.PixelBufferNotify(rec.Record{
				SentAt:       time.Now().UTC(),
				CampaignCode: msg.CampaignCode,
				ServiceCode:  msg.ServiceCode,
				Tid:          msg.Tid,
				Pixel:        val,
			}); err != nil {
				logCtx.WithFields(log.Fields{
					"error": err.Error(),
				}).Error("send pixel")
			}
		}
	}

	autoClickInfo := struct {
		AutoClick bool
	}{
		AutoClick: campaignByLink[campaignLink].CanAutoClick,
	}

	campaignByLink[campaignLink].SimpleServe(c, autoClickInfo)

	m.CampaignAccess.Inc()
	m.Success.Inc()

	// finish. Here is autoclick goes
	if !cnf.Service.OnClickNewSubscription {
		return
	}
	if !campaignByLink[campaignLink].CanAutoClick {
		return
	}

	action = rbmq.UserActionsNotify{
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
		action.CampaignCode = msg.CampaignCode

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
			"tid":           msg.Tid,
			"link":          campaignLink,
			"hash":          campaignByLink[campaignLink].Hash,
			"msisdn":        msg.Msisdn,
			"campaign_code": campaignByLink[campaignLink].Code,
		}).Info("added new subscritpion due to ratio")
	}

	return
}
