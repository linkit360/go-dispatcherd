package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	inmem_service "github.com/linkit360/go-inmem/service"
)

// gather information from headers, etc
func gatherInfo(c *gin.Context, campaign inmem_service.Campaign) (msg rbmq.AccessCampaignNotify) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	r := c.Request
	headers, err := json.Marshal(r.Header)
	if err != nil {
		logCtx.Error("cannot marshal headers")
		headers = []byte("{}")
	}
	msg = rbmq.AccessCampaignNotify{
		Tid:          tid,
		UserAgent:    r.UserAgent(),
		Referer:      r.Referer(),
		UrlPath:      r.URL.String(),
		Method:       r.Method,
		Headers:      string(headers),
		CampaignId:   campaign.Id,
		ServiceId:    campaign.ServiceId,
		CampaignHash: campaign.Hash,
		Supported:    true,
		CountryCode:  cnf.Service.CountryCode,
		OperatorCode: cnf.Service.OperatorCode,
	}

	// but for now we use get parameter to pass msisdn
	// and there not always could be the correct IP adress
	// so, if operator code or country code not found
	// we can set them via msisdn
	var ok bool
	if msg.Msisdn, ok = c.GetQuery("msisdn"); ok && len(msg.Msisdn) >= 5 {
		logCtx.WithFields(log.Fields{
			"msisdn": msg.Msisdn,
		}).Debug("took from get params")
	} else {
		msg.Msisdn = sessions.GetFromSession("msisdn", c)
		if len(msg.Msisdn) >= 5 {
			logCtx.WithFields(log.Fields{
				"msisdn": msg.Msisdn,
			}).Debug("took from session")
		}
	}

	if err := detectByIpInfo(c, &msg); err != nil {
		return msg
	}
	if !msg.Supported {
		logCtx.WithField("IP", msg.IP).Debug("is not supported")
		msg.Error = "Not supported"
		return msg
	}

	if len(msg.Msisdn) > 5 {
		return msg
	}

	logCtx.WithFields(log.Fields{
		"msisdn": msg.Msisdn,
	}).Debug("msisdn is empty")
	msg.Error = "Msisdn not found"

	return msg
}

//get all IP addresses
//get supported IP-s
// in common, this branch of code in action
func detectByIpInfo(c *gin.Context, msg *rbmq.AccessCampaignNotify) error {

	if !cnf.Service.DetectByIpEnabled {
		return nil
	}

	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})

	IPs := getIPAdress(c.Request)
	if len(IPs) == 0 {
		return nil

	}
	infos, err := inmem_client.GetIPInfoByIps(IPs)
	if err != nil {
		logCtx.WithField("error", err.Error()).Error("cannot get ip infos")
		return nil
	}
	if len(infos) == 0 {
		logCtx.WithField("error", "no ip info").Error("cannot get ip info")
		return nil
	}

	info := inmem_service.GetSupportedIPInfo(infos)
	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if msg.Supported == false {
		return nil
	}

	log.WithFields(log.Fields{
		"ip":            info.IP,
		"operator_code": info.OperatorCode,
		"supported":     info.Supported,
		"headers":       info.MsisdnHeaders,
	}).Debug("got IP info")

	for _, header := range info.MsisdnHeaders {

		msisdn := c.Request.Header.Get(header)
		if len(msisdn) > 5 {
			log.WithFields(log.Fields{
				"msisdn": msisdn,
			}).Debug("found in header")
			msg.Msisdn = msisdn
			return nil
		}
		msisdn = os.Getenv(header)
		if len(msisdn) > 0 {
			log.WithFields(log.Fields{
				"msisdn": msisdn,
			}).Debug("found in environment")
			msg.Msisdn = msisdn
			return nil
		}
	}

	if msg.Msisdn == "" {
		return nil
	}
	info, err = inmem_client.GetIPInfoByMsisdn(msg.Msisdn)
	if err != nil {
		err = fmt.Errorf("operator.GetInfoByMsisdn: %s", err.Error())

		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{
			"error": err.Error(),
		}).Debug("cannot find info by msisdn")
		return nil
	}

	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	logCtx.WithFields(log.Fields{
		"msisdn": msg.Msisdn,
		"code":   msg.OperatorCode,
	}).Debug("found matched operator")
	return nil
}

func getIPAdress(r *http.Request) []net.IP {
	result := []net.IP{}

	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
		addresses := strings.Split(r.Header.Get(h), ",")
		for i := len(addresses) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(addresses[i])
			realIP := net.ParseIP(ip)
			if !realIP.IsGlobalUnicast() {
				continue
			}
			result = append(result, realIP)
		}
	}
	return result
}
