package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content_client "github.com/linkit360/go-contentd/rpcclient"
	content_service "github.com/linkit360/go-contentd/server/src/service"
	"github.com/linkit360/go-dispatcherd/src/config"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	mid_client "github.com/linkit360/go-mid/rpcclient"
	mid "github.com/linkit360/go-mid/service"
	redirect_client "github.com/linkit360/go-partners/rpcclient"
	redirect_service "github.com/linkit360/go-partners/service"
	"github.com/linkit360/go-utils/rec"
	"github.com/linkit360/go-utils/structs"
)

// file for global variables,
// initialisation
// common functions

var cnf config.AppConfig
var e *gin.Engine
var notifierService rbmq.Notifier

var campaignByLink map[string]*mid.Campaign
var campaignByHash map[string]mid.Campaign

func Init(conf config.AppConfig, engine *gin.Engine) {
	log.SetLevel(log.DebugLevel)

	cnf = conf
	e = engine

	if err := content_client.Init(conf.ContentClient); err != nil {
		log.Fatal("cannot init contentd client")
	}
	if err := mid_client.Init(conf.MidConfig); err != nil {
		log.Fatal("cannot init mid client")
	}
	if err := redirect_client.Init(conf.RedirectConfig); err != nil {
		log.Fatal("cannot redirect client")
	}
	initBeeline()

	UpdateCampaigns()
	notifierService = rbmq.NewNotifierService(conf.Notifier)
}

func SaveState() {
	beelineSaveState()
}

func AccessHandler(c *gin.Context) {
	m.Access.Inc()

	begin := time.Now()
	c.Next()

	responseTime := time.Since(begin)
	tid := sessions.GetTid(c)

	if len(c.Errors) > 0 {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"error":  c.Errors.String(),
			"since":  responseTime,
		}).Error(c.Errors.String())
	} else {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"since":  responseTime,
		}).Info("access")
	}
	c.Header("X-Response-Time", responseTime.String())
}

// update campaign list
// when campaign changes, update request comes to mid service
// and from mid service it goes to dispatcher
func UpdateCampaigns() error {
	log.WithFields(log.Fields{}).Debug("get all campaigns")
	campaigns, err := mid_client.GetAllCampaigns()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot add campaign handlers")
		return err
	}

	for key, _ := range campaignByLink {
		delete(campaignByLink, key)
	}
	for key, _ := range campaignByHash {
		delete(campaignByHash, key)
	}
	campaignByLink = make(map[string]*mid.Campaign, len(campaigns))
	campaignByHash = make(map[string]mid.Campaign, len(campaigns))
	for _, campaign := range campaigns {
		camp := campaign
		campaignByLink[campaign.Link] = &camp
		campaignByHash[campaign.Hash] = campaign
	}

	campaignsJson, _ := json.Marshal(campaignByLink)
	log.WithFields(log.Fields{
		"len": len(campaigns),
		"c":   string(campaignsJson),
	}).Info("campaigns updated")
	return nil
}

type EventNotify struct {
	EventName string                          `json:"event_name,omitempty"`
	EventData redirect_service.DestinationHit `json:"event_data,omitempty"`
}

// traffic redirect
func trafficRedirect(r structs.AccessCampaignNotify, c *gin.Context) {
	if r.CountryCode == 0 {
		r.CountryCode = cnf.Service.CountryCode
	}
	if r.OperatorCode == 0 {
		r.OperatorCode = cnf.Service.OperatorCode
	}
	hit := redirect_service.DestinationHit{
		SentAt: time.Now().UTC(),
		Tid:    r.Tid,
		Msisdn: r.Msisdn,
	}
	defer func() {
		notifierService.RedirectNotify(hit)
	}()

	dst, err := redirect_client.GetDestination(redirect_service.GetDestinationParams{
		CountryCode:  r.CountryCode,
		OperatorCode: r.OperatorCode,
	})
	if err != nil {
		log.WithFields(log.Fields{
			"tid":   r.Tid,
			"error": err.Error(),
		}).Error("cann't get redirect url from tr")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 302)
		return
	}

	mid_client.IncRedirectStatCount(dst.DestinationId)

	hit.DestinationId = dst.DestinationId
	hit.PartnerId = dst.PartnerId
	hit.Destination = dst.Destination
	hit.PricePerHit = dst.PricePerHit
	hit.CountryCode = dst.CountryCode
	hit.OperatorCode = dst.OperatorCode
	m.TrafficRedirectSuccess.Inc()
	log.WithFields(log.Fields{
		"tid": r.Tid,
		"url": dst.Destination,
	}).Info("traffic redirect")
	http.Redirect(c.Writer, c.Request, dst.Destination, 302)
}

// redirect inside dispatcher to another campaign if he/she already was here
func redirect(msg structs.AccessCampaignNotify) (campaign mid.Campaign, err error) {
	if !cnf.Service.Rejected.CampaignRedirectEnabled {
		log.WithFields(log.Fields{
			"tid": msg.Tid,
		}).Debug("redirect off")
		campaign.Code = msg.CampaignCode
		return
	}

	// if nextCampaignCode == msg.CampaignCode then it's not rejected msisdn
	campaign.Code, err = mid_client.GetMsisdnCampaignCache(msg.CampaignCode, msg.Msisdn)
	if err != nil {
		err = fmt.Errorf("mid_client.GetMsisdnCampaignCache: %s", err.Error())
		log.WithFields(log.Fields{
			"tid":   msg.Tid,
			"error": err.Error(),
		}).Debug("redirect check faieled")
		return
	}

	if campaign.Code == msg.CampaignCode {
		log.WithFields(log.Fields{
			"tid": msg.Tid,
		}).Debug("no redirect: ok")
		return
	}
	// no more campaigns
	if campaign.Code == "" {
		m.Rejected.Inc()

		log.WithFields(log.Fields{
			"tid":      msg.Tid,
			"msisdn":   msg.Msisdn,
			"campaign": msg.CampaignCode,
		}).Debug("redirect")
		return
	}

	campaign, err = mid_client.GetCampaignByCode(campaign.Code)
	if err != nil {
		err = fmt.Errorf("mid_client.GetCampaignById: %s", err.Error())

		log.WithFields(log.Fields{
			"tid":    msg.Tid,
			"msisdn": msg.Msisdn,
			"error":  err.Error(),
		}).Debug("redirect")
		return
	}
	log.WithFields(log.Fields{
		"tid":       msg.Tid,
		"msisdn":    msg.Msisdn,
		"campaign":  msg.CampaignCode,
		"2campaign": campaign.Code,
	}).Debug("redirect")
	return
}

// generate unique url for user
// the unique url creation is inside contentd service
func generateUniqueUrl(r structs.AccessCampaignNotify) (url string, err error) {
	logCtx := log.WithFields(log.Fields{
		"tid": r.Tid,
	})
	service, err := mid_client.GetServiceByCode(r.ServiceCode)
	if err != nil {
		m.UnknownService.Inc()

		err = fmt.Errorf("mid_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceCode,
			"error":     err.Error(),
		}).Error("cannot get service by id")
		return
	}
	contentProperties, err := content_client.GetUniqueUrl(content_service.GetContentParams{
		Msisdn:       r.Msisdn,
		Tid:          r.Tid,
		ServiceCode:  r.ServiceCode,
		CampaignCode: r.CampaignCode,
		OperatorCode: r.OperatorCode,
		CountryCode:  r.CountryCode,
	})

	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()
		err = fmt.Errorf("content_client.GetUniqueUrl: %s", contentProperties.Error)
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceCode,
			"error":     err.Error(),
		}).Error("contentd internal error")
		return
	}
	if err != nil {
		err = fmt.Errorf("content_client.GetUniqueUrl: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceCode,
			"error":     err.Error(),
		}).Error("cannot get unique content url")
		return
	}
	url = fmt.Sprintf(service.SMSOnContent, cnf.Server.Url+"/u/"+contentProperties.UniqueUrl)
	return
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
	var msg structs.AccessCampaignNotify
	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignCode = msg.CampaignCode
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
	msg.CountryCode = cnf.Service.LandingPages.Mobilink.CountryCode
	msg.OperatorCode = cnf.Service.LandingPages.Mobilink.OperatorCode
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

	//service, err := mid_client.GetServiceById(msg.ServiceId)
	//if err != nil {
	//	err = fmt.Errorf("mid_client.GetServiceById: %s", err.Error())
	//	logCtx.WithFields(log.Fields{
	//		"error":      err.Error(),
	//		"service_id": msg.ServiceId,
	//	}).Error("cannot get service by id")
	//	c.JSON(500, gin.H{"error": "Cannot get service"})
	//	return
	//}
	// generate code

	//r := rec.Record{
	//	Msisdn:             msg.Msisdn,
	//	Tid:                msg.Tid,
	//	SubscriptionStatus: "",
	//	CountryCode:        msg.CountryCode,
	//	OperatorCode:       msg.OperatorCode,
	//	Publisher:          sessions.GetFromSession("publisher", c),
	//	Pixel:              sessions.GetFromSession("pixel", c),
	//	CampaignCode:         msg.CampaignCode,
	//	ServiceId:          msg.ServiceId,
	//	DelayHours:         service.DelayHours,
	//	PaidHours:          service.PaidHours,
	//	KeepDays:           service.KeepDays,
	//	Price:              100 * int(service.Price),
	//	SMSText:            "Your code: " + code,
	//	Type:             code,
	//}
	//
	//mobilinkCodeCache.SetDefault(msg.Msisdn, r)
	//notifierService.Notify("send_sms", cnf.Service.LandingPages.Mobilink.Queues.SMS, r)
	c.JSON(200, gin.H{"message": "Sent"})
}

func verifyCode(c *gin.Context) {
	var r rec.Record

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
	defer func() {
		action.Msisdn = r.Msisdn
		action.CampaignCode = r.CampaignCode
		action.Tid = r.Tid
		if err != nil {
			action.Error = err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("code verify")
		}
		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("code verify notify user action")
		}
	}()

	//recI, ok := mobilinkCodeCache.Get(r.Msisdn)
	var recI interface{}
	var ok bool
	if !ok {
		err = fmt.Errorf("msisdn code not found: %s", r.Msisdn)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get code for msisdn")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	r, ok = recI.(rec.Record)
	if !ok {
		err = fmt.Errorf("code cache type %T, expected %T", recI, rec.Record{})

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get code for msisdn")
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	code, _ := c.GetQuery("code")

	if r.Type != code {
		err = fmt.Errorf("Code is incorrect: %v, expected %v", code, r.Type)
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("wrong code")
		c.JSON(500, gin.H{"error": err.Error(), "message": "wrong code"})
		return
	}

	if err = notifierService.NewSubscriptionNotify(cnf.Service.LandingPages.Mobilink.Queues.MO, r); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("notify new subscription")

		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	contentUrl, err := createUniqueUrl(r)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// XXX: check content url
	r.SMSText = fmt.Sprintf("%s", contentUrl)
	//if err = notifierService.Notify(cnf.Service.LandingPages.Mobilink.Queues.SMS, "content", r); err != nil {
	//	logCtx.WithField("error", err.Error()).Error("send content")
	//	return
	//}
	c.JSON(200, gin.H{"message": "content sent"})
	return
}
