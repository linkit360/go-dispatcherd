package handlers

import (
	"fmt"
	"io/ioutil"
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

var cnf config.AppConfig
var notifierService rbmq.Notifier
var contentClient *content.Client

func Init(conf config.AppConfig) {
	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Server.RBMQQueueName, conf.RBMQ)

	var err error
	contentClient, err = content.NewClient(conf.ContentClient.DSN, conf.ContentClient.Timeout)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Init content service rpc client")
	}
}

// uniq links generation ??
// operators check
func HandlePull(c *gin.Context) {
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

	contentProperties, err := contentClient.Get(msisdn, campaignHash)
	if err != nil {
		err := fmt.Errorf("contentClient.Get: %s", err.Error())
		c.Error(err)
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	notifierService.NewSubscriptionNotify(contentProperties)

	serveContentFile(contentProperties.ContentPath, c)
}

func serveContentFile(filePath string, c *gin.Context) {
	w := c.Writer

	content, err := ioutil.ReadFile(cnf.Subscriptions.StaticPath + filePath)
	if err != nil {
		err := fmt.Errorf("serveContentFile. ioutil.ReadFile: %s", err.Error())
		c.Error(err)
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset-utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(200)
	w.Write(content)
}
