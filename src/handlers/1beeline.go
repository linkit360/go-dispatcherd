package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	cache "github.com/patrickmn/go-cache"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-mid/rpcclient"
	rec "github.com/linkit360/go-utils/rec"
)

func AddBeelineHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Beeline.Enabled {
		return
	}
	e.Group("/lp").GET(":campaign_link", AccessHandler, notifyBeeline, serveCampaigns)
	e.Group("/campaign/:campaign_link").GET("", AccessHandler, redirectUserBeeline)
	log.WithFields(log.Fields{}).Debug("beeline handlers init")
}

var beelineCache *cache.Cache

func initBeeline() {
	if !cnf.Service.LandingPages.Beeline.Enabled {
		return
	}
	beelineSessions, err := ioutil.ReadFile(cnf.Service.LandingPages.Beeline.SessionPath)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
			"pid":   os.Getpid(),
		}).Debug("load sessions")

		beelineCache = cache.New(15*time.Minute, time.Minute)
		return
	}

	var cacheItems map[string]cache.Item
	if err = json.Unmarshal(beelineSessions, &cacheItems); err != nil {
		log.WithFields(log.Fields{
			"error":    err.Error(),
			"sessions": beelineCache,
			"pid":      os.Getpid(),
		}).Error("load")
		beelineCache = cache.New(15*time.Minute, time.Minute)
		return
	}

	beelineCache = cache.NewFrom(15*time.Minute, time.Minute, cacheItems)
	log.WithFields(log.Fields{
		"len": len(cacheItems),
		"pid": os.Getpid(),
	}).Debug("load")
}

func beelineSaveState() {
	if !cnf.Service.LandingPages.Beeline.Enabled {
		return
	}
	beelineSessions, err := json.Marshal(beelineCache.Items())
	if err != nil {
		log.WithFields(log.Fields{
			"error": fmt.Errorf("json.Marshal: %s", err.Error()),
			"len":   beelineCache.Items(),
			"pid":   os.Getpid(),
		}).Error("beeline save session")
		return
	}
	if err := ioutil.WriteFile(cnf.Service.LandingPages.Beeline.SessionPath, beelineSessions, 0666); err != nil {
		log.WithFields(log.Fields{
			"error": fmt.Errorf("ioutil.WriteFile: %s", err.Error()),
			"pid":   os.Getpid(),
		}).Error("beeline save session")

		return
	}

	log.WithFields(log.Fields{
		"len": len(beelineCache.Items()),
		"pid": os.Getpid(),
	}).Info("beeline save session ok")
}

func notifyBeeline(c *gin.Context) {
	if !cnf.Service.LandingPages.Beeline.Enabled {
		return
	}
	log.WithFields(log.Fields{}).Debug("beeline notify...")

	var err error
	var tid string
	var notifyBeelineUrl string
	var status string
	var land rec.Record

	action := rbmq.UserActionsNotify{
		Action: "pull_click",
		Tid:    tid,
	}
	defer func() {
		fields := log.Fields{}
		if tid != "" {
			fields["tid"] = tid
		}
		if notifyBeelineUrl != "" {
			fields["req"] = notifyBeelineUrl
		}
		if status != "" {
			fields["status"] = status
		}
		if err == nil {
			fields["success"] = true
			log.WithFields(fields).Info("notify")
		} else {
			fields["error"] = err.Error()
			log.WithFields(fields).Error("notify")
		}

		action.CampaignCode = land.CampaignCode
		action.Tid = land.Tid

		if err := notifierService.ActionNotify(action); err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   land.Tid,
			}).Error("notify user action")
		} else {
			log.WithFields(log.Fields{
				"tid": land.Tid,
			}).Info("sent to beeline")
		}

	}()

	serviceId, _ := c.GetQuery("serviceId")
	if serviceId == "" {
		err = fmt.Errorf("ServiceId not found%s", "")
		return
	}
	landI, found := beelineCache.Get(serviceId)
	if !found {
		err = fmt.Errorf("ServiceId not found: %v", serviceId)
		return
	}
	var ok bool
	land, ok = landI.(rec.Record)
	if !ok {
		err = fmt.Errorf("Wrong type: %T", landI)
		return
	}
	tid = land.Tid

	notifyBeelineUrl = cnf.Service.LandingPages.Beeline.Url + "?serviceId=" + serviceId
	req, err := http.NewRequest("GET", notifyBeelineUrl, nil)
	if err != nil {
		err = fmt.Errorf("Beeline Notify: Cann't create request: %s, url: %s", err.Error(), notifyBeelineUrl)
		return
	}
	req.Close = false
	httpClient := http.Client{
		Timeout: time.Duration(cnf.Service.LandingPages.Beeline.Timeout) * time.Second,
	}
	req.SetBasicAuth(cnf.Service.LandingPages.Beeline.Auth.User, cnf.Service.LandingPages.Beeline.Auth.Pass)

	resp, err := httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Beeline Notify: httpClient.Do: %s, url: %s", err.Error(), notifyBeelineUrl)
		return
	}
	status = resp.Status

	if resp.StatusCode != 200 {
		err = fmt.Errorf("Beeline Notify: status: %s, url: %s", resp.Status, notifyBeelineUrl)
		return
	}
	beelineCache.Delete(serviceId)

	if err = notifierService.NewSubscriptionNotify(cnf.Service.LandingPages.Beeline.MOQueue, land); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		log.WithFields(log.Fields{
			"error": err.Error(),
			"tid":   land.Tid,
		}).Error("notify new subscription")
		return
	}
	return
}

func redirectUserBeeline(c *gin.Context) {
	var r rec.Record
	var err error
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	var msg rbmq.AccessCampaignNotify

	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
	}
	defer func() {
		if err != nil {
			m.Errors.Inc()

			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   msg.Tid,
			}).Error("beeline redirect to lp")
		}
		action.Msisdn = msg.Msisdn
		action.CampaignCode = msg.CampaignCode
		action.Tid = msg.Tid

		if err := notifierService.ActionNotify(action); err != nil {
			log.WithFields(log.Fields{
				"error": err.Error(),
				"tid":   r.Tid,
			}).Error("notify action error")
		} else {
			log.WithFields(log.Fields{
				"tid": msg.Tid,
			}).Info("notify action ok")
		}
		if errAccessCampaign := notifierService.AccessCampaignNotify(msg); errAccessCampaign != nil {
			log.WithFields(log.Fields{
				"tid":   r.Tid,
				"error": errAccessCampaign.Error(),
				"msg":   fmt.Sprintf("%#v", msg),
			}).Error("notify access campaign error")
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
	msg = gatherInfo(c, *campaign)
	msg.CountryCode = cnf.Service.LandingPages.Beeline.CountryCode
	msg.OperatorCode = cnf.Service.LandingPages.Beeline.OperatorCode

	service, err := inmem_client.GetServiceByCode(msg.ServiceCode)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		log.WithFields(log.Fields{
			"tid":        msg.Tid,
			"error":      err.Error(),
			"service_id": msg.ServiceCode,
		}).Error("cannot get service by id")
		return
	}

	v := url.Values{}
	v.Add("tid", r.Tid)
	// we will parse parameters inserted who came
	contentUrl := campaign.PageThankYou
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
	}).Debug("got resp")

	if resp.StatusCode != 302 {
		err = fmt.Errorf("Status code: %d", resp.StatusCode)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	// save to cache
	msg.UrlPath = resp.Header.Get("Location")
	u, err := url.Parse(msg.UrlPath)
	if err != nil {
		err = fmt.Errorf("Cannot get service id: %d", err.Error())
		return
	}
	serviceId := u.Query().Get("serviceId")

	beelineCache.SetDefault(serviceId, rec.Record{
		Tid:                      tid,
		CountryCode:              cnf.Service.LandingPages.Beeline.CountryCode,
		OperatorCode:             cnf.Service.LandingPages.Beeline.OperatorCode,
		SentAt:                   time.Now().UTC(),
		CampaignCode:             campaign.Code,
		ServiceCode:              campaign.ServiceCode,
		Publisher:                sessions.GetFromSession("publisher", c),
		Pixel:                    sessions.GetFromSession("pixel", c),
		DelayHours:               service.DelayHours,
		PaidHours:                service.PaidHours,
		RetryDays:                service.RetryDays,
		Price:                    100 * int(service.Price),
		Periodic:                 true,
		PeriodicDays:             service.PeriodicDays,
		PeriodicAllowedFromHours: service.PeriodicAllowedFrom,
		PeriodicAllowedToHours:   service.PeriodicAllowedTo,
		SMSText:                  "",
	})

	http.Redirect(c.Writer, c.Request, msg.UrlPath, 303)
	return
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
		Action:       "return_back",
		Tid:          tid,
		CampaignCode: campaign.Code,
		Msisdn:       sessions.GetFromSession("msisdn", c),
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
