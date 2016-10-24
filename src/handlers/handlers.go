package handlers

import (
	"errors"
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
	"github.com/vostrok/dispatcherd/src/sessions"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier
var contentClient *content.Client

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf
	notifierService = rbmq.NewNotifierService(conf.Notifier)

	var err error
	contentClient, err = content.NewClient(conf.ContentClient.DSN, conf.ContentClient.Timeout)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Init content service rpc client")
	}
}

// uniq links generation ??
func HandlePull(c *gin.Context) {
	tid := sessions.GetTid(c)
	logCtx := log.WithField("tid", tid)

	var msg rbmq.AccessCampaignNotify
	var action rbmq.ActionNotify
	var err error

	defer func(msg rbmq.AccessCampaignNotify, action rbmq.ActionNotify, err error) {
		if err := notifierService.AccessCampaignNotify(msg); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify access campaign")
		}
		action.Error = err
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		}
	}(msg, action, err)
	action = rbmq.ActionNotify{
		Action: "pull_click",
		Tid:    tid,
	}

	msg = rbmq.AccessCampaignNotify{
		UserAgent: c.Request.UserAgent(),
		Referer:   c.Request.Referer(),
		UrlPath:   c.Request.URL.String(),
		Method:    c.Request.Method,
		Headers:   fmt.Sprintf("%v", c.Request.Header),
	}

	ip := getIPAdress(c.Request)
	if ip == nil {
		err = errors.New("Cannot determine IP address")
		logCtx.Error("cannot determine IP address")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	info := operator.GetIpInfo(ip)
	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if !info.Supported {
		err = errors.New("Not supported")
		logCtx.WithFields(log.Fields{"info": info}).Error("operator is not supported")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	msisdn := c.Request.Header.Get(info.Header)
	if len(msisdn) == 0 {
		err = errors.New("Msisdn not found")
		logCtx.WithField("Header", info.Header).Error("msisdn is empty")
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	msg.Msisdn = msisdn
	logCtx = logCtx.WithField("msisdn", msisdn)

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Subscriptions.CampaignHashLength {
		logCtx.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")
		err := errors.New("Wrong campaign length")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithField("campaignHash", campaignHash)

	contentProperties, err := contentClient.Get(service.GetUrlByCampaignHashParams{
		Msisdn:       msisdn,
		Tid:          tid,
		CampaignHash: campaignHash,
		CountryCode:  info.CountryCode,
		OperatorCode: info.OperatorCode,
	})
	if err != nil {
		err := fmt.Errorf("contentClient.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		msg.ContentServiceError = true
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignId = contentProperties.CampaignId
	msg.ContentId = contentProperties.ContentId
	msg.ServiceId = contentProperties.ServiceId

	// todo one time url-s
	if err = serveContentFile(contentProperties.ContentPath, c); err != nil {
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		msg.ContentFileError = true
		metrics.M.ContentDeliveryError.Add(1)
		http.Redirect(c.Writer, c.Request, cnf.Subscriptions.ErrorRedirectUrl, 303)
	}
}

func serveContentFile(filePath string, c *gin.Context) error {
	w := c.Writer

	content, err := ioutil.ReadFile(cnf.Server.StaticPath + filePath)
	if err != nil {
		err := fmt.Errorf("ioutil.ReadFile: %s", err.Error())
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
