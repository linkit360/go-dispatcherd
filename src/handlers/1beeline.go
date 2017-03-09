package handlers

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
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
	contentUrl := cnf.Server.Url + "/campaign/" + campaign.Hash + "/" + campaign.PageSuccess + v.Encode()
	forwardURL := cnf.Server.Url + "/campaign/" + campaign.Hash + "/" + campaign.PageError + v.Encode()

	v.Add("flagSubscribe", "True")
	v.Add("contentUrl", url.QueryEscape(contentUrl))
	v.Add("forwardURL", url.QueryEscape(forwardURL))
	reqUrl := cnf.Service.LandingPages.Beeline.AisUrl + v.Encode()

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
		Timeout: time.Duration(cnf.Service.LandingPages.Beeline.Timeout) * time.Second,
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Cann't make request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	beelineResponse, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("ioutil.ReadAll: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	defer resp.Body.Close()
	log.WithFields(log.Fields{
		"tid":    r.Tid,
		"url":    reqUrl,
		"status": resp.Status,
		"body":   string(beelineResponse),
	}).Debug("got resp")

	if resp.StatusCode != 200 {
		err = fmt.Errorf("Status code: %d", resp.StatusCode)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	operator, err := inmem_client.GetOperatorByCode(msg.OperatorCode)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetOperatorByCode: %s", err.Error())
		log.WithFields(log.Fields{
			"tid":   msg.Tid,
			"error": err.Error(),
		}).Error("cannot get operator by code")
	} else {
		for _, header := range operator.MsisdnHeaders {
			msg.Msisdn = c.Request.Header.Get(header)
			if len(msg.Msisdn) > 7 {
				log.WithFields(log.Fields{
					"msisdn": msg.Msisdn,
				}).Debug("found in header")
				sessions.Set("msisdn", msg.Msisdn, c)
				sessions.Set("tid", msg.Tid, c)
				break
			}
		}
	}

	sArray := strings.SplitN(string(beelineResponse), "serviceId=", 2)
	if len(sArray) < 2 {
		err = fmt.Errorf("strings.SplitN: %s", "cannot determine service id")
		log.WithFields(log.Fields{
			"tid":   r.Tid,
			"url":   reqUrl,
			"body":  string(beelineResponse),
			"error": err.Error(),
		}).Debug("cannot redirect to OP LP")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	r.OperatorToken = sArray[1]

	http.Redirect(c.Writer, c.Request, strings.SplitN(string(beelineResponse), "Location: ", 2)[1], 303)
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
