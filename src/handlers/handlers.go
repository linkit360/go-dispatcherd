package handlers

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"fmt"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/content"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

var cnf config.AppConfig
var notifierService rbmq.Notifier

func Init(conf config.AppConfig) {
	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Server.RBMQQueueName, conf.RBMQ)
}

func HandleSubscription(c *gin.Context) {

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Subscriptions.CampaignHashLength {
		log.WithFields(log.Fields{"campaignHash": campaignHash, "length": len(campaignHash)}).Error("Length is too small")
		err := fmt.Errorf("Wrong campaign length %v", len(campaignHash))
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	log.WithField("campaignHash", campaignHash)

	hash, err := content.GetHashBySubscriptionLink(campaignHash)
	if err != nil {
		err := fmt.Errorf("GetHashBySubscriptionLink %s", err.Error())
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	notifierService.NewSubscriptionNotify(rbmq.NewSubscriptionMessage{CampaignHash: campaignHash, ContentId: 0})

	http.ServeFile(c.Writer, c.Request, cnf.Subscriptions.StaticPath+hash)
}
