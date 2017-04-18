package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-utils/rec"
	cache "github.com/patrickmn/go-cache"
)

//VOSTOK user flow (confirmed by Alain):
//
//1 User came on VOSTOK LP.(Приешл на ЛП)
//
//2 User presses DOWNLOAD button. (Нажал скачать)
//
//3 User see POPUP window on the SAME LP with OK and CANCEL buttons (попап с двумя кнопками)
//
//3.1 User presses CANCEL -> NOTHING happens. (ничего не происхожит)
//
//3.2 User presses OK -> initiated subscription and charging (создается daily подписка и начинается тарификация за первый день)
//
//4 User REDIRECTED to content immediately after point 3.2. ( Мы можем первый раз отдать после подтверждения подписки контент через редирект пользователя. Все последующие дги по подписке отдавать контент через СМС)
//
//Purge flow
//
//1 По умолчанию подписка создается на 7 дней.
//
//2 Если в течении подписки 3 подряд неуспешные тарифицкации - подписка анулируется.
//
//3 (Если контент скачан хотя бы 2 раза в течении первыйх 7 дней, то пользователь считается активным)
//
//4 Если пользователь активный, то подписка продливается на 8ой день.
//
//5 Если 8ой день была успешная тарификация, то подписка продливается на 9ый день.
//
//6 Если 9ый день была успешная тарификация, то полписка продливается на 10ый день.
//
//7 Мы информируем через СМС абонента только в случае подписки и отписки.
//
//8 Нет ретраев вообще на данный момент.

func init() {
	MobilinkInitCache()
}
func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}

	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	e.Group("/lp/:campaign_link").GET("ok", AccessHandler, ContentSubscribe)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
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
			time.Duration(cacheConf.ExpirationHours)*time.Hour,
			time.Duration(cacheConf.ExpirationHours)*time.Hour,
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
			time.Duration(cacheConf.ExpirationHours)*time.Hour,
			time.Duration(cacheConf.ExpirationHours)*time.Hour,
		)
		return
	}

	mobilinkCodeCache = cache.NewFrom(
		time.Duration(cacheConf.ExpirationHours)*time.Hour,
		time.Duration(cacheConf.ExpirationHours)*time.Hour,
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

	mobilinkCacheCodesJSON, err := json.Marshal(mobilinkCodeCache.Items())
	if err != nil {
		log.WithFields(log.Fields{
			"error": fmt.Errorf("json.Marshal: %s", err.Error()),
			"len":   beelineCache.Items(),
			"pid":   os.Getpid(),
		}).Error("mobilink save session")
		return
	}

	if err := ioutil.WriteFile(cnf.Service.LandingPages.Mobilink.Cache.Path, mobilinkCacheCodesJSON, 0666); err != nil {
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

// subscribes and adds new subscription
func ContentSubscribe(c *gin.Context) {
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
		return
	}
	msg = gatherInfo(c, campaign)
	if msg.Error != "" {
		return
	}
	if err = startNewSubscription(c, msg); err != nil {
		err = fmt.Errorf("startNewSubscription: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	contentUrl, err := createUniqueUrl(r)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	m.Success.Inc()

	http.Redirect(c.Writer, c.Request, contentUrl, 303)
}
