package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	mid_client "github.com/linkit360/go-mid/rpcclient"
	"github.com/linkit360/go-utils/rec"
)

func AddCampaignHandler(rg *gin.RouterGroup) {
	if !cnf.Service.LandingPages.Custom {
		e.HEAD("/lp/:campaign_link/*filepath", ServeStatic)
		e.GET("/lp/:campaign_link/*filepath", ServeStatic)
	}
	e.GET("/updateTemplates", updateTemplates)
}

func updateTemplates(c *gin.Context) {
	UpdateCampaigns()
	c.JSON(200, struct{}{})
}

func ServeStatic(c *gin.Context) {
	filePath := c.Params.ByName("filepath")
	log.WithFields(log.Fields{
		"fp":   filePath,
		"link": c.Params.ByName("campaign_link"),
	}).Info("path")

	if filePath == "" || filePath == "/" || filePath == "//" {
		serveCampaigns(c)
		return
	}
	campaignLink := c.Params.ByName("campaign_link")
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.PageNotFoundError.Inc()
		err := fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"link":  campaignLink,
			"path":  c.Request.URL.Path,
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		c.JSON(500, gin.H{"error": "link not found"})
		return
	}

	filePath = cnf.Server.Path + "campaign/" + campaign.Id + filePath
	log.WithField("path", filePath).Debug("serve file")

	c.File(filePath)
}

func serveCampaigns(c *gin.Context) {
	msg := gatherInfo(c)
	logCtx := log.WithFields(log.Fields{
		"tid": msg.Tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    msg.Tid,
	}
	m.Incoming.Inc()

	var err error
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

	campaignLink := c.Params.ByName("campaign_link")

	// important, do not use campaign from this operation
	// bcz we need to inc counter to process ratio
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		logCtx.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignId = campaign.Id
	msg.ServiceCode = campaign.ServiceCode
	msg.CampaignHash = campaign.Hash
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
		isRejected, err := mid_client.IsMsisdnRejectedByService(msg.ServiceCode, msg.Msisdn)
		if err != nil {
			err = fmt.Errorf("mid_client.IsMsisdnRejectedByService: %s", err.Error())
			logCtx.WithFields(log.Fields{
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
		logCtx.WithFields(log.Fields{
			"err": msg.Error,
		}).Debug("gather info failed")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	if cnf.Service.SendRestorePixelEnabled {
		val, ok := c.GetQuery("aff_sub")
		if ok && len(val) >= 5 {
			if err := notifierService.PixelBufferNotify(rec.Record{
				SentAt:      time.Now().UTC(),
				CampaignId:  msg.CampaignId,
				ServiceCode: msg.ServiceCode,
				Tid:         msg.Tid,
				Pixel:       val,
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

	actionAutoClick := rbmq.UserActionsNotify{
		Action: "autoclick",
	}

	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
			logCtx.WithFields(log.Fields{
				"msisdn": msg.Msisdn,
				"link":   campaignLink,
				"error":  err.Error(),
			}).Info("error add new subscription")
		}
		actionAutoClick.Tid = msg.Tid
		actionAutoClick.Msisdn = msg.Msisdn
		actionAutoClick.CampaignId = msg.CampaignId

		if err := notifierService.ActionNotify(actionAutoClick); err != nil {
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("notify user action")
		}
	}()

	if err = startNewSubscription(c, msg); err == nil {
		logCtx.WithFields(log.Fields{
			"msisdn":      msg.Msisdn,
			"campaign_id": campaignByLink[campaignLink].Id,
		}).Info("added new subscritpion due to ratio")
	} else {
		logCtx.WithFields(log.Fields{
			"error":       err.Error(),
			"campaign_id": campaignByLink[campaignLink].Id,
		}).Info("cannot add new subscription")
	}

	return
}
