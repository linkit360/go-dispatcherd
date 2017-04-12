package handlers

import (
	"crypto/aes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content_client "github.com/linkit360/go-contentd/rpcclient"
	content_service "github.com/linkit360/go-contentd/service"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	"github.com/linkit360/go-utils/rec"
)

func AddQRTechHandlers() {
	if cnf.Service.LandingPages.QRTech.Enabled {
		e.Group("/lp/:campaign_link", AccessHandler).GET("", qrTechHandler)
		log.WithFields(log.Fields{}).Debug("qrtech handlers init")
	}
}

func qrTechHandler(c *gin.Context) {
	var err error
	tid := sessions.GetTid(c)
	m.Incoming.Inc()

	var msg = rbmq.AccessCampaignNotify{}
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
	}
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})

	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("serve campaign")
		} else {
			logCtx.WithFields(log.Fields{}).Error("serve ok")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("notify user action")
		}
		if errAccessCampaign := notifierService.AccessCampaignNotify(msg); errAccessCampaign != nil {
			logCtx.WithFields(log.Fields{
				"error": errAccessCampaign.Error(),
				"msg":   fmt.Sprintf("%#v", msg),
			}).Error("notify access campaign")
		}

		val, ok := c.GetQuery("aff_sub")
		if ok && len(val) >= 5 {
			log.WithFields(log.Fields{
				"tid": tid,
			}).Debug("found pixel in get params")
			if err := notifierService.PixelBufferNotify(rec.Record{
				SentAt:     time.Now().UTC(),
				CampaignId: msg.CampaignId,
				Tid:        msg.Tid,
				Pixel:      val,
			}); err != nil {
				logCtx.WithFields(log.Fields{
					"error": err.Error(),
				}).Error("send pixel")
			}
		}
	}()

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]

	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.Errors.Inc()
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg = gatherInfo(c, *campaign)
	if msg.IP == "" {
		m.IPNotFoundError.Inc()
	}
	if !msg.Supported {
		m.NotSupported.Inc()
	}

	msg.CampaignId = campaign.Id
	msg.ServiceId = campaign.ServiceId

	v := url.Values{}
	v.Add("SHORTCODE", strconv.FormatInt(campaign.ServiceId, 10))
	v.Add("SP_CONTENT", cnf.Service.LandingPages.QRTech.ContentUrl+"/get")
	reqUrl := ""

	telco, _ := c.GetQuery("telco")
	operatorCode := int64(0)
	if telco == "dtac" {
		operatorCode = 52005
	} else if telco == "ais" {
		operatorCode = 52001
	}
	msg.OperatorCode = operatorCode

	if campaign.CanAutoClick {
		logCtx.WithFields(log.Fields{}).Debug("autoclick enabled")
		service, err := inmem_client.GetServiceById(msg.ServiceId)
		if err != nil {
			err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
			logCtx.WithFields(log.Fields{
				"error":      err.Error(),
				"service_id": msg.ServiceId,
			}).Error("cannot get service by id")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		r := rec.Record{
			Msisdn:             msg.Msisdn,
			Tid:                msg.Tid,
			SubscriptionStatus: "",
			CountryCode:        msg.CountryCode,
			OperatorCode:       msg.OperatorCode,
			Publisher:          sessions.GetFromSession("publisher", c),
			Pixel:              sessions.GetFromSession("pixel", c),
			CampaignId:         msg.CampaignId,
			ServiceId:          msg.ServiceId,
			DelayHours:         service.DelayHours,
			PaidHours:          service.PaidHours,
			KeepDays:           service.KeepDays,
			Price:              100 * int(service.Price),
		}
		contentProperties, err := content_client.GetUniqueUrl(content_service.GetContentParams{
			Msisdn:         r.Msisdn,
			Tid:            r.Tid,
			ServiceId:      r.ServiceId,
			CampaignId:     r.CampaignId,
			OperatorCode:   r.OperatorCode,
			CountryCode:    r.CountryCode,
			SubscriptionId: r.SubscriptionId,
		})

		if contentProperties.Error != "" {
			err = fmt.Errorf("content_client.GetUniqueUrl: %s", contentProperties.Error)
			logCtx.WithFields(log.Fields{
				"serviceId": r.ServiceId,
				"error":     err.Error(),
			}).Error("contentd internal error")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		if err != nil {
			err = fmt.Errorf("content_client.GetUniqueUrl: %s", err.Error())
			logCtx.WithFields(log.Fields{
				"serviceId": r.ServiceId,
				"error":     err.Error(),
			}).Error("cannot get unique content url")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}

		contentUrl := cnf.Service.LandingPages.QRTech.ContentUrl + contentProperties.UniqueUrl
		aesBlock, err := aes.NewCipher([]byte(cnf.Service.LandingPages.QRTech.AesKey))
		var contentUrlEncrypted []byte
		aesBlock.Encrypt(contentUrlEncrypted, []byte(contentUrl))
		v.Add("content_url", string(contentUrlEncrypted))
		v.Add("telco", telco)
		if telco == "dtac" {
			v.Add("telco_url", cnf.Service.LandingPages.QRTech.DtacUrl)
		}
		if telco == "ais" {
			v.Add("telco_url", cnf.Service.LandingPages.QRTech.AisUrl)
		}
		reqUrl = cnf.Service.LandingPages.QRTech.AutoclickUrl + "?" + v.Encode()
		logCtx.WithFields(log.Fields{
			"content_url":           contentUrl,
			"content_url_encrypted": string(contentUrlEncrypted),
			"result_url":            reqUrl,
		}).Debug("send to autoclick")

		http.Redirect(c.Writer, c.Request, reqUrl, 303)
		return
	}

	if telco == "dtac" || msg.OperatorCode == int64(52005) { // dtac
		reqUrl = cnf.Service.LandingPages.QRTech.DtacUrl + "?" + v.Encode()
		log.WithFields(log.Fields{
			"operator": "dtac",
			"url":      reqUrl,
		}).Debug("call")
		http.Redirect(c.Writer, c.Request, reqUrl, 303)
		return
	}

	if telco == "ais" || msg.OperatorCode == int64(52001) { // ais
		reqUrl = cnf.Service.LandingPages.QRTech.AisUrl + "?" + v.Encode()
		log.WithFields(log.Fields{
			"operator": "ais",
			"url":      reqUrl,
		}).Info("determined")
	} else {
		log.WithFields(log.Fields{
			"error": "cannot determine operator",
		}).Error("cannot determine operator")
		reqUrl = cnf.Service.LandingPages.QRTech.AisUrl + "?" + v.Encode()
	}

	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		err = fmt.Errorf("Cann't create request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	req.Close = false
	httpClient := http.Client{
		Timeout: time.Duration(cnf.Service.LandingPages.QRTech.Timeout) * time.Second,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Cann't make request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
	}
	if resp.StatusCode > 220 {
		err = fmt.Errorf("qrTech resp status: %s", resp.Status)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	qrTechResponse, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("ioutil.ReadAll: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	defer resp.Body.Close()

	logCtx.WithFields(log.Fields{
		"response": string(qrTechResponse),
		"len":      len(string(qrTechResponse)),
	}).Debug("got response")

	if telco == "ais" {
		start := strings.Index(string(qrTechResponse), "url=http") + 4
		if start < 0 {
			err = fmt.Errorf("cannot parse response start: %s", string(qrTechResponse))
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		end := strings.Index(string(qrTechResponse), `">`)
		if end < 0 {
			err = fmt.Errorf("cannot parse response end: %s", string(qrTechResponse))
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		x := string(qrTechResponse)
		parsedUrl := x[start:end]
		http.Redirect(c.Writer, c.Request, parsedUrl, 303)
		return
	}
}
