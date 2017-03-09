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

	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
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
		CountryCode:  cnf.Service.CountryCode,
		OperatorCode: cnf.Service.OperatorCode,
	}

	//get all IP addresses
	//get supported IP-s
	// in common, this branch of code in action
	flagFoundIpInfo := false
	IPs := getIPAdress(r)
	if len(IPs) != 0 {
		infos, err := inmem_client.GetIPInfoByIps(IPs)
		if err != nil {
			logCtx.Debug("cannot get ip infos")
			err = nil
		}
		if len(infos) > 0 {
			info := inmem_service.GetSupportedIPInfo(infos)
			if info.Supported == false {
				logCtx.WithField("ips", IPs).Debug("cannot determine IP address")
			} else {
				log.WithFields(log.Fields{
					"ip":            info.IP,
					"operator_code": info.OperatorCode,
					"supported":     info.Supported,
					"headers":       info.MsisdnHeaders,
				}).Debug("got IP info")

				flagFoundIpInfo = true
				msg.IP = info.IP
				msg.OperatorCode = info.OperatorCode
				msg.CountryCode = info.CountryCode
				msg.Supported = info.Supported

				msg.Msisdn = ""
				for _, header := range info.MsisdnHeaders {

					msg.Msisdn = r.Header.Get(header)
					if len(msg.Msisdn) > 0 {
						log.WithFields(log.Fields{
							"msisdn": msg.Msisdn,
						}).Debug("found in header")
						return msg
					}
					msg.Msisdn = os.Getenv(header)
					if len(msg.Msisdn) > 0 {
						log.WithFields(log.Fields{
							"msisdn": msg.Msisdn,
						}).Debug("found in environment")
						return msg
					}
				}
			}
		}

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

	// we worked hard and haven't found msisdn
	if len(msg.Msisdn) <= 5 {
		logCtx.WithFields(log.Fields{
			"msisdn": msg.Msisdn,
		}).Debug("msisdn is empty")

		msg.Error = "Msisdn not found"
		return msg
	}

	if flagFoundIpInfo {
		return msg
	}

	info, err := inmem_client.GetIPInfoByMsisdn(msg.Msisdn)
	if err != nil {
		err = fmt.Errorf("operator.GetInfoByMsisdn: %s", err.Error())

		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{
			"error": err.Error(),
		}).Debug("cannot find info by msisdn")
		return msg
	}
	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if !info.Supported {
		msg.Error = "Not supported"
		logCtx.WithFields(log.Fields{
			"info": info,
		}).Debug("operator is not supported")
		return msg
	}
	logCtx.WithFields(log.Fields{
		"msisdn": msg.Msisdn,
		"code":   msg.OperatorCode,
	}).Debug("found matched operator")
	return
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
