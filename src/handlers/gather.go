package handlers

// gather information from headers, etc
import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
)

func gatherInfo(tid, campaignHash string, c *gin.Context) (msg rbmq.AccessCampaignNotify, err error) {
	logCtx := log.WithFields(log.Fields{"tid": tid, "campaign": campaignHash})

	r := c.Request
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

	//for _, e := range os.Environ() {
	//	log.WithFields(log.Fields{
	//		"tid": tid,
	//	}).Debug(e)
	//}

	//get all IP addresses
	//get supported IP-s
	// in common, this branch of code in action
	flagFoundIpInfo := false
	IPs := getIPAdress(r)
	if len(IPs) != 0 {
		infos, err := inmem_client.GetIPInfoByIps(IPs)
		if err != nil {
			m.IPNotFoundError.Inc()
			logCtx.Debug("cannot get ip infos")
		}
		if len(infos) > 0 {
			info := inmem_service.GetSupportedIPInfo(infos)
			if info.Supported == false {
				m.IPNotFoundError.Inc()
				logCtx.Debug("cannot determine IP address")
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
						return msg, nil
					}
					msg.Msisdn = os.Getenv(header)
					if len(msg.Msisdn) > 0 {
						log.WithFields(log.Fields{
							"msisdn": msg.Msisdn,
						}).Debug("found in environment")
						return msg, nil
					}
					log.WithFields(log.Fields{
						"header": header,
					}).Debug("msisdn not found")
				}
			}
		}

	}

	// but for now we use get parameter to pass msisdn
	// and there not always could be the correct IP adress
	// so, if operator code or country code not found
	// we can dset them via msisdn
	var ok bool
	if msg.Msisdn, ok = c.GetQuery("msisdn"); ok {
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
		m.MsisdnNotFoundError.Inc()
		err = errors.New("Msisdn not found")
		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{
			"Header": r.Header,
		}).Debug("msisdn is empty")
		return msg, errors.New("Msisdn not found")
	}

	if flagFoundIpInfo {
		return msg, nil
	}

	info, err := inmem_client.GetIPInfoByMsisdn(msg.Msisdn)
	if err != nil {
		m.GetInfoByMsisdnError.Inc()

		err = fmt.Errorf("operator.GetInfoByMsisdn: %s", err.Error())
		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{}).Debug("cannot find info by msisdn")
		return msg, err
	}
	msg.IP = info.IP
	msg.OperatorCode = info.OperatorCode
	msg.CountryCode = info.CountryCode
	msg.Supported = info.Supported

	if !info.Supported {
		m.NotSupported.Inc()
		err = errors.New("Not supported")
		msg.Error = err.Error()
		logCtx.WithFields(log.Fields{"info": info}).Error("operator is not supported")
		return msg, err
	}

	logCtx.WithFields(log.Fields{
		"msisdn": msg.Msisdn,
	}).Debug("took from prefixes table")
	return
}

func getIPAdress(r *http.Request) []net.IP {
	result := []net.IP{}

	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
		addresses := strings.Split(r.Header.Get(h), ",")
		for i := len(addresses) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(addresses[i])
			realIP := net.ParseIP(ip)
			if !realIP.IsGlobalUnicast() { //|| IsPrivateSubnet(realIP)
				// bad address, go to next
				continue
			}
			result = append(result, realIP)
		}
	}
	return result
}
