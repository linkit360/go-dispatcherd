package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-utils/rec"
	cache "github.com/patrickmn/go-cache"
)

func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}

	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	e.Group("/lp/:campaign_link").GET("ok", AccessHandler, ContentSubscribe)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}

// subscribes and adds new subscription
func ContentSubscribe(c *gin.Context) {
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

	contentUrl, err := createUniqueUrl(r)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	m.Success.Inc()

	http.Redirect(c.Writer, c.Request, contentUrl, 303)
}
