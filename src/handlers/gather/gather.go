package gather

import (
	"errors"
	"net"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"

	"encoding/json"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

func Gather(tid, campaignHash string, r *http.Request) (msg rbmq.AccessCampaignNotify, err error) {
	logCtx := log.WithFields(log.Fields{"tid": tid, "campaign": campaignHash})

	headers, err := json.Marshal(r.Header)
	if err != nil {
		logCtx.Error("cannot marshal headers")
		headers = []byte("{}")
	}
	msg = rbmq.AccessCampaignNotify{
		Tid:          tid,
		CampaignHash: campaignHash,
		UserAgent:    r.UserAgent(),
		Referer:      r.Referer(),
		UrlPath:      r.URL.String(),
		Method:       r.Method,
		Headers:      string(headers),
	}

	ip := getIPAdress(r)
	if ip == nil {
		err = errors.New("Cannot determine IP address")
		msg.Error = err.Error()
		logCtx.Error("cannot determine IP address")
		return
	}
	info := operator.GetIpInfo(ip)
	log.WithFields(log.Fields{
		"operator_code": info.OperatorCode,
		"supported":     info.Supported,
	}).Debug("got IP info")

	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if !info.Supported {
		err = errors.New("Not supported")
		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{"info": info}).Error("operator is not supported")
		return
	}
	msisdn := ""
	for _, header := range info.MsisdnHeaders {
		msisdn = r.Header.Get(header)
		log.WithFields(log.Fields{
			"request": r.Header,
			"header":  header,
			"msisdn":  msisdn,
		}).Debug("check header")

		if len(msisdn) > 0 {
			break
		}
	}
	if len(msisdn) == 0 {
		err = errors.New("Msisdn not found")
		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{
			"operatorsSettings": info.MsisdnHeaders,
			"Header":            r.Header,
		}).Error("msisdn is empty")
	}
	msg.Msisdn = msisdn
	return
}

func getIPAdress(r *http.Request) net.IP {
	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
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
