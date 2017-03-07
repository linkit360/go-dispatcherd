package handlers

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/utils/rec"
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

	contentUrl, err := generateUniqueUrl(msg)
	if err != nil {
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	v := url.Values{}
	v.Add("SHORTCODE", strconv.FormatInt(campaign.ServiceId, 10))
	v.Add("SP_CONTENT", contentUrl)
	reqUrl := cnf.Service.LandingPages.QRTech.Url + "?" + v.Encode()
	logCtx.WithFields(log.Fields{
		"url": reqUrl,
	}).Debug("call")

	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		err = fmt.Errorf("Cann't create request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	req.Close = false
	httpClient := http.Client{
		Timeout: time.Duration(cnf.Service.LandingPages.Beeline.Timeout) * time.Second,
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

	http.Redirect(c.Writer, c.Request, parsedUrl, 303)
}
