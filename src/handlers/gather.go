package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/linkit360/go-dispatcherd/src/sessions"
	mid "github.com/linkit360/go-mid/service"
	"github.com/linkit360/go-utils/structs"
)

// gather information from headers, etc
func gatherInfo(c *gin.Context, campaign mid.Campaign) (msg structs.AccessCampaignNotify) {
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
	msg = structs.AccessCampaignNotify{
		Tid:          tid,
		IP:           r.Header.Get("X-Forwarded-For"),
		UserAgent:    r.UserAgent(),
		Referer:      r.Referer(),
		UrlPath:      r.URL.String(),
		Method:       r.Method,
		Headers:      string(headers),
		CampaignCode: campaign.Code,
		ServiceCode:  campaign.ServiceCode,
		CampaignHash: campaign.Hash,
		Supported:    true,
		CountryCode:  cnf.Service.CountryCode,
		OperatorCode: cnf.Service.OperatorCode,
	}

	logCtx.WithFields(log.Fields{
		"urlpath": c.Request.URL.Path + "?" + c.Request.URL.RawQuery,
		"url":     r.URL.String(),
	}).Debug("log")

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
	if len(msg.Msisdn) < 5 {
		msg.Error = "Msisdn not found"
	}

	IPs := getIPAdress(c.Request)
	msg.IP = strings.Join(IPs, ", ")

	return msg
}

func getIPAdress(r *http.Request) []string {
	result := []string{}

	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
		addresses := strings.Split(r.Header.Get(h), ",")
		for i := len(addresses) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(addresses[i])
			realIP := net.ParseIP(ip)
			if !realIP.IsGlobalUnicast() {
				continue
			}
			result = append(result, realIP.String())
		}
	}
	return result
}
