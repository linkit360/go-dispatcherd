package handlers

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier
var contentClient *content.Client

func Init(conf config.AppConfig) {
	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Notifier)

	var err error
	contentClient, err = content.NewClient(conf.ContentClient.DSN, conf.ContentClient.Timeout)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Init content service rpc client")
	}
}

// uniq links generation ??
// operators check
func HandlePull(c *gin.Context) {
	var msg rbmq.AccessCampaignNotify
	defer func(msg rbmq.AccessCampaignNotify) {
		notifierService.AccessCampaignNotify(msg)
	}(msg)
	msg = rbmq.AccessCampaignNotify{
		UserAgent: c.Request.UserAgent(),
		Referer:   c.Request.Referer(),
		UrlPath:   c.Request.URL.String(),
		Method:    c.Request.Method,
		Headers:   fmt.Sprintf("%v", c.Request.Header),
	}

	// todo: when other operators - could be another header name
	msisdn := c.Request.Header.Get("X-Parse-MSISDN")
	if len(msisdn) == 0 {
		log.WithField("Header", "X-Parse-MSISDN").Error("msisdn is empty")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	msg.Msisdn = msisdn

	ip := getIPAdress(c.Request)
	if ip == nil {
		log.WithField("msisdn", msisdn).Error("cannot determine IP address")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	info := operator.GetIpInfo(ip)

	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if !info.Supported {
		log.WithFields(log.Fields{"msisdn": msisdn, "info": info}).Error("operator is not supported")
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

	contentProperties, err := contentClient.Get(service.GetUrlByCampaignHashParams{
		Msisdn:       msisdn,
		CampaignHash: campaignHash,
		CountryCode:  info.CountryCode,
		OperatorCode: info.OperatorCode,
	})
	if err != nil {
		msg.ContentServiceError = true
		err := fmt.Errorf("contentClient.Get: %s", err.Error())
		c.Error(err)
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}

	// for future use: after growth and createing of subscription service
	// for PULL workflow there are no need to handle subscription
	//notifierService.NewSubscriptionNotify(contentProperties)

	notifierService.NewSubscriptionNotify(contentProperties)

	if err = serveContentFile(contentProperties.ContentPath, c); err != nil {
		msg.ContentFileError = true
	}
}

func serveContentFile(filePath string, c *gin.Context) error {
	w := c.Writer

	content, err := ioutil.ReadFile(cnf.Subscriptions.StaticPath + filePath)
	if err != nil {
		err := fmt.Errorf("serveContentFile. ioutil.ReadFile: %s", err.Error())
		c.Error(err)
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset-utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(200)
	w.Write(content)
	return nil
}

func getIPAdress(r *http.Request) net.IP {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		addresses := strings.Split(r.Header.Get(h), ",")
		for i := len(addresses) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(addresses[i])
			realIP := net.ParseIP(ip)
			if !realIP.IsGlobalUnicast() || operator.IsPrivateSubnet(realIP) {
				// bad address, go to next
				continue
			}
			return realIP
		}
	}
	return nil
}
