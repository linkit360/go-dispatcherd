package handlers

import (
	"fmt"
	"net/http"
	"net/url"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	rec "github.com/vostrok/utils/rec"
	"io/ioutil"
	"strings"
)

func redirectUserBeeline(c *gin.Context) {
	var r rec.Record
	var err error
	var msg rbmq.AccessCampaignNotify
	action := rbmq.UserActionsNotify{
		Action: "pull_click",
	}
	defer func() {
		if err != nil {
			m.Errors.Inc()

			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("handle pull")
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
		err = fmt.Errorf("Cann't find campaign by hash: %s", campaignHash)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	v := url.Values{}
	v.Add("tid", r.Tid)
	// we will parse parameters inserted who came
	contentUrl := cnf.Server.Url + "/campaign/" + campaign.Hash + "/" + campaign.PageSuccess + v.Encode()
	forwardURL := cnf.Server.Url + "/campaign/" + campaign.Hash + "/" + campaign.PageError + v.Encode()

	v.Add("flagSubscribe", "True")
	v.Add("contentUrl", url.QueryEscape(contentUrl))
	v.Add("forwardURL", url.QueryEscape(forwardURL))
	reqUrl := cnf.Service.LandingPages.Beeline.Url + v.Encode()

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
		Timeout: cnf.Service.LandingPages.Beeline.Timeout,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Cann't make request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
	}
	beelineResponse, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("ioutil.ReadAll: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	defer resp.Body.Close()
	log.WithFields(log.Fields{
		"tid":  r.Tid,
		"url":  reqUrl,
		"body": string(beelineResponse),
	}).Debug("got resp")

	sArray := strings.SplitN(beelineResponse, "serviceId=", 2)
	if len(sArray) < 2 {
		err = fmt.Errorf("strings.SplitN: %s", "cannot determine service id")
		log.WithFields(log.Fields{
			"tid":   r.Tid,
			"url":   reqUrl,
			"body":  string(beelineResponse),
			"error": err.Error(),
		}).Debug("cannot redirect to OP LP")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
	}
	r.OperatorToken = sArray[1]

	http.Redirect(c.Writer, c.Request, strings.SplitN(beelineResponse, "Location: ", 2)[1], 303)
}

//	rg := e.Group("/campaign/:campaign_hash")
// rg.GET("/:campaign_page", handlers.AccessHandler, handlers.CampaignPage)
func CampaignPage(c *gin.Context) {
	var err error
	tid, ok := c.GetQuery("tid")
	if ok && len(tid) >= 10 {
		log.WithFields(log.Fields{
			"tid": tid,
		}).Debug("found tid in get params")

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
