package handlers

import (
	"fmt"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

var cnf config.AppConfig
var notifierService rbmq.Notifier
var contentClient content.Client

func Init(conf config.AppConfig) {
	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Server.RBMQQueueName, conf.RBMQ)

	var err error
	contentClient, err = content.NewClient(conf.ContentClient.DSN, conf.ContentClient.Timeout)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Init content service rpc client")
	}
}

func HandleSubscription(c *gin.Context) {
	// todo: when other operators - could be another header name
	msisdn := c.Request.Header.Get("X-Parse-MSISDN")
	if len(msisdn) == 0 {
		log.WithField("Header", "X-Parse-MSISDN").Error("msisdn is empty")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Subscriptions.CampaignHashLength {
		log.WithFields(log.Fields{"campaignHash": campaignHash, "length": len(campaignHash)}).Error("Length is too small")
		err := fmt.Errorf("Wrong campaign length %v", len(campaignHash))
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	log.WithField("campaignHash", campaignHash)

	hash, err := contentClient.Get(msisdn, campaignHash)
	if err != nil {
		err := fmt.Errorf("contentClient.Get: %s", err.Error())
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	notifierService.NewSubscriptionNotify(rbmq.NewSubscriptionMessage{CampaignHash: campaignHash, ContentId: 0})

	http.ServeFile(c.Writer, c.Request, cnf.Subscriptions.StaticPath+hash)
}
