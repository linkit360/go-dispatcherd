package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	cache "github.com/patrickmn/go-cache"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	inmem_client "github.com/linkit360/go-inmem/rpcclient"
	"github.com/linkit360/go-utils/rec"
)

func init() {
	MobilinkInitCache()
}
func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}

	e.Group("/lp/:campaign_link", AccessHandler).GET("/generate", generateCode)
	e.Group("/lp/:campaign_link", AccessHandler).GET("/veryfy", verifyCode)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}

func generateCode(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "generate_code",
		Tid:    tid,
	}
	m.Incoming.Inc()

	var err error
	var msg rbmq.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("code generate")
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

	// important, do not use campaign from this operation
	// bcz we need to inc counter to process ratio
	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	msg = gatherInfo(c, *campaign)
	if msg.IP == "" {
		m.IPNotFoundError.Inc()
	}
	if msg.Error == "Msisdn not found" {
		m.MsisdnNotFoundError.Inc()
		c.JSON(500, gin.H{"error": msg.Error})
		return
	}
	if !msg.Supported {
		m.NotSupported.Inc()
		c.JSON(500, gin.H{"error": "Not supported"})
		return
	}

	service, err := inmem_client.GetServiceById(msg.ServiceId)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error":      err.Error(),
			"service_id": msg.ServiceId,
		}).Error("cannot get service by id")
		return err
	}
	// generate code
	code := "123"
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
		SMSText:            "Your code: " + code,
		Notice:             code,
	}
	mobilinkCodeCache.SetDefault(msg.Msisdn, r)
	notifierService.Notify("send_sms", cnf.Service.LandingPages.Mobilink.SMSCodeQueueName, r)
}

func verifyCode(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	action := rbmq.UserActionsNotify{
		Action: "verify_code",
		Tid:    tid,
	}
	m.Incoming.Inc()

	var err error
	var msg rbmq.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("serve campaign")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("notify user action")
		}
	}()

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]
	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	msg = gatherInfo(c, *campaign)

	// todo: verify code
	recI, ok := mobilinkCodeCache.Get(msg.Msisdn)
	if !ok {
		err = fmt.Errorf("msisdn code not found: %s", msg.Msisdn)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get code for msisdn")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	r, ok := recI.(rec.Record)
	if !ok {
		err = fmt.Errorf("code cache type %T, expected %T", recI, rec.Record{})

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get code for msisdn")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	code, _ := c.GetQuery("code")

	if r.Notice != code {
		err = fmt.Errorf("Code is incorrect: %v, expected %v", code, r.Notice)
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("wrong code")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	if err = startNewSubscription(c, msg); err == nil {
		log.WithFields(log.Fields{
			"tid":        msg.Tid,
			"link":       campaignLink,
			"hash":       campaignByLink[campaignLink].Hash,
			"msisdn":     msg.Msisdn,
			"campaignid": campaignByLink[campaignLink].Id,
		}).Info("added new subscritpion due to ratio")
	}

	return
}

var mobilinkCodeCache *cache.Cache

func MobilinkInitCache() {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}
	cacheConf := cnf.Service.LandingPages.Mobilink.Cache
	mobilinkCodeCacheJson, err := ioutil.ReadFile(cacheConf.Path)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
			"pid":   os.Getpid(),
		}).Debug("load sessions")

		mobilinkCodeCache = cache.New(
			time.Duration(cacheConf.ExpirationHours*time.Hour),
			time.Duration(cacheConf.ExpirationHours*time.Hour),
		)
		return
	}

	var cacheItems map[string]cache.Item
	if err = json.Unmarshal(mobilinkCodeCacheJson, &cacheItems); err != nil {
		log.WithFields(log.Fields{
			"error":    err.Error(),
			"sessions": beelineCache,
			"pid":      os.Getpid(),
		}).Error("load")
		mobilinkCodeCache = cache.New(
			time.Duration(cacheConf.ExpirationHours*time.Hour),
			time.Duration(cacheConf.ExpirationHours*time.Hour),
		)
		return
	}

	mobilinkCodeCache = cache.NewFrom(
		time.Duration(cacheConf.ExpirationHours*time.Hour),
		time.Duration(cacheConf.ExpirationHours*time.Hour),
		cacheItems,
	)
	log.WithFields(log.Fields{
		"len": len(cacheItems),
		"pid": os.Getpid(),
	}).Debug("load")
}

func mobilinkISaveState() {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}

	movilinkCacheCodesJSON, err := json.Marshal(mobilinkCodeCache.Items())
	if err != nil {
		log.WithFields(log.Fields{
			"error": fmt.Errorf("json.Marshal: %s", err.Error()),
			"len":   beelineCache.Items(),
			"pid":   os.Getpid(),
		}).Error("mobilink save session")
		return
	}

	if err := ioutil.WriteFile(cnf.Service.LandingPages.Mobilink.Cache.Path, movilinkCacheCodesJSON, 0666); err != nil {
		log.WithFields(log.Fields{
			"error": fmt.Errorf("ioutil.WriteFile: %s", err.Error()),
			"pid":   os.Getpid(),
		}).Error("mobilink save session")

		return
	}

	log.WithFields(log.Fields{
		"len": len(beelineCache.Items()),
		"pid": os.Getpid(),
	}).Info("mobilink save session ok")
}
