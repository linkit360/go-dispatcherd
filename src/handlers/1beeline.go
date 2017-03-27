package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"encoding/json"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	rec "github.com/vostrok/utils/rec"
)

func AddBeelineHandlers(e *gin.Engine) {
	if cnf.Service.LandingPages.Beeline.Enabled {
		e.Group("/lp").GET(":campaign_link", AccessHandler, redirectUserBeeline)
		e.Group("/campaign/:campaign_hash").GET("/:campaign_page", AccessHandler, returnBackCampaignPage)
		log.WithFields(log.Fields{}).Debug("beeline handlers init")
	}
}

func redirectUserBeeline(c *gin.Context) {
	var r rec.Record
	var err error
	tid := sessions.GetTid(c)
	if tid == "" {
		tid = rec.GenerateTID()
	}
	msg := rbmq.AccessCampaignNotify{
		CountryCode:  cnf.Service.CountryCode,
		OperatorCode: cnf.Service.OperatorCode,
		Tid:          tid,
	}
	action := rbmq.UserActionsNotify{
		Action: "pull_click",
		Tid:    tid,
	}
	defer func() {
		if err != nil {
			m.Errors.Inc()

			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("beeline redirect to lp")
		}
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid

		if err := notifierService.ActionNotify(action); err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("notify user action")
		} else {
			log.WithFields(log.Fields{
				"tid": r.Tid,
			}).Info("sent to beeline")
		}
	}()

	campaignLink := c.Params.ByName("campaign_link")
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.CampaignLinkWrong.Inc()
		err = fmt.Errorf("Cann't find campaign by link: %s", campaignLink)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	msg.CampaignId = campaign.Id
	msg.ServiceId = campaign.ServiceId

	v := url.Values{}
	v.Add("tid", r.Tid)
	// we will parse parameters inserted who came
	contentUrl := "http://ru.slypee.com/bgetcontent%"
	forwardURL := cnf.Server.Url + "/campaign/" + campaign.Hash + "/" + campaign.PageError + v.Encode()

	v.Add("flagSubscribe", "True")
	v.Add("contentUrl", contentUrl)
	v.Add("forwardURL", forwardURL)
	reqUrl := cnf.Service.LandingPages.Beeline.Url + "?" + v.Encode()

	log.WithFields(log.Fields{
		"tid": r.Tid,
		"url": reqUrl,
	}).Debug("call api")

	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		err = fmt.Errorf("Cann't create request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	req.Close = false
	req.SetBasicAuth(cnf.Service.LandingPages.Beeline.Auth.User, cnf.Service.LandingPages.Beeline.Auth.Pass)

	httpClient := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Duration(cnf.Service.LandingPages.Beeline.Timeout) * time.Second,
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Cann't make request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	headers, err := json.Marshal(resp.Header)
	if err != nil {
		log.Error("cannot marshal headers")
		headers = []byte("{}")
	}
	defer resp.Body.Close()
	log.WithFields(log.Fields{
		"tid":     r.Tid,
		"url":     reqUrl,
		"status":  resp.Status,
		"headers": string(headers),
		//"body":   string(beelineResponse),
	}).Debug("got resp")

	if resp.StatusCode != 302 {
		err = fmt.Errorf("Status code: %d", resp.StatusCode)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg.UrlPath = resp.Header.Get("Location")
	http.Redirect(c.Writer, c.Request, msg.UrlPath, 303)
}

// rg := e.Group("/campaign/:campaign_hash")
// rg.GET("/:campaign_page", handlers.AccessHandler, handlers.CampaignPage)
func returnBackCampaignPage(c *gin.Context) {
	var err error
	tid, ok := c.GetQuery("tid")
	if ok && len(tid) >= 10 {
		log.WithFields(log.Fields{
			"tid": tid,
		}).Debug("found tid in get params")
	} else {
		tid = sessions.GetFromSession("tid", c)
		log.WithFields(log.Fields{
			"tid": tid,
		}).Debug("found tid in session")
	}
	defer func() {
		if err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   tid,
			}).Error("return back error")
		}
	}()

	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		m.CampaignHashWrong.Inc()

		err := fmt.Errorf("Wrong campaign length: len %d, %s", len(campaignHash), campaignHash)
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	campaign, ok := campaignByHash[campaignHash]
	if !ok {
		m.CampaignHashWrong.Inc()
		err = fmt.Errorf("Cann't find campaign: %s", campaignHash)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	action := rbmq.UserActionsNotify{
		Action:     "return_back",
		Tid:        tid,
		CampaignId: campaign.Id,
		Msisdn:     sessions.GetFromSession("msisdn", c),
	}
	campaignPage := c.Params.ByName("campaign_page")
	for _, v := range campaignByHash {
		if campaignPage == v.PageSuccess {
			action.Action = "charge_paid"
			break
		}
		if campaignPage == v.PageError {
			action.Action = "charge_failed"
			break
		}
	}
	if err = notifierService.ActionNotify(action); err != nil {
		return
	}
	c.HTML(http.StatusOK, campaignPage+".html", nil)
}
