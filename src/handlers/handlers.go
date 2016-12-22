package handlers

// handlers of dispatcher:
// handle pull
// handle get content
// handle campaigns
// access notify handler
// access middleware
import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	content_service "github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/config"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/dispatcherd/src/utils"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
	queue_config "github.com/vostrok/utils/config"
	"github.com/vostrok/utils/rec"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier

var campaignByLink map[string]*inmem_service.Campaign

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf

	content.Init(conf.ContentClient)
	if err := inmem_client.Init(conf.InMemConfig); err != nil {
		log.Fatal("cannot init inmem client")
	}
	UpdateCampaignByLink()
	notifierService = rbmq.NewNotifierService(conf.Notifier)
}

func UpdateCampaignByLink() error {
	log.WithFields(log.Fields{}).Debug("get all campaigns")
	campaigns, err := inmem_client.GetAllCampaigns()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot add campaign handlers")
		return err
	}

	for key, _ := range campaignByLink {
		delete(campaignByLink, key)
	}
	campaignByLink = make(map[string]*inmem_service.Campaign, len(campaigns))
	for _, campaign := range campaigns {
		camp := campaign
		campaignByLink[campaign.Link] = &camp
	}
	log.WithFields(log.Fields{
		"len": len(campaigns),
		"c":   fmt.Sprintf("%#v", campaignByLink),
	}).Info("campaigns updated")
	return nil
}

func HandlePull(c *gin.Context) {
	campaignHash := getCampaignHash(c)
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		log.Error("unknown hash")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	log.Debug("handle pull_click")
	if _, err := startNewSubscription(c, campaignHash); err != nil {
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
}

func startNewSubscription(c *gin.Context, campaignHash string) (r rec.Record, err error) {
	m.Agree.Inc()
	sessions.SetSession(c)
	tid := sessions.GetTid(c)

	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})

	logCtx.Debug("start new subscription")

	action := rbmq.UserActionsNotify{
		Action: "pull_click",
		Tid:    tid,
	}
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		} else {
		}
		sessions.RemoveTid(c)
	}()
	msg, err := gatherInfo(tid, c)
	if err != nil {
		action.Error = err.Error()
		return
	}
	msg.CampaignHash = campaignHash
	action.Msisdn = msg.Msisdn
	logCtx = logCtx.WithField("msisdn", msg.Msisdn)

	campaign, err := inmem_client.GetCampaignByHash(campaignHash)
	if err != nil {
		m.CampaignHashWrong.Inc()

		action.Error = err.Error()
		logCtx.WithFields(log.Fields{
			"error": err.Error(),
			"hash":  campaignHash,
		}).Error("cannot get campaign by hash")
		return
	}
	action.CampaignId = campaign.Id

	service, err := inmem_client.GetServiceById(campaign.ServiceId)
	if err != nil {
		m.ServiceError.Inc()

		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error":      err.Error(),
			"service_id": campaign.ServiceId,
		}).Error("cannot get service by id")
		return
	}
	r = rec.Record{
		Msisdn:             msg.Msisdn,
		Tid:                tid,
		SubscriptionStatus: "",
		CountryCode:        msg.CountryCode,
		OperatorCode:       msg.OperatorCode,
		Publisher:          sessions.GetFromSession("publisher", c),
		Pixel:              sessions.GetFromSession("pixel", c),
		CampaignId:         campaign.Id,
		ServiceId:          campaign.ServiceId,
		DelayHours:         service.DelayHours,
		PaidHours:          service.PaidHours,
		KeepDays:           service.KeepDays,
		Price:              100 * int(service.Price),
	}
	if service.SendNotPaidTextEnabled {
		r.SMSSend = true
		r.SMSText = service.NotPaidText
	}

	operator, err := inmem_client.GetOperatorByCode(msg.OperatorCode)
	if err != nil {
		m.OperatorNameError.Inc()

		err = fmt.Errorf("inmem_client.GetOperatorByCode: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"error": err.Error(),
			"code":  msg.OperatorCode,
		}).Error("cannot get operator by code")
		return
	}
	queue := queue_config.NewSubscriptionQueueName(operator.Name)
	logCtx.WithField("queue", queue).Debug("inform new subscritpion")
	if err = notifierService.NewSubscriptionNotify(queue, r); err != nil {
		m.NotifyNewSubscriptionError.Inc()

		err = fmt.Errorf("notifierService.NewSubscriptionNotify: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("notify new subscription")
		return
	}

	m.AgreeSuccess.Inc()
	m.Success.Inc()
	return
}

// same as handle pull, but do not create subscription
// and has different metrics in the end
func ContentGet(c *gin.Context) {
	m.CampaignAccess.Inc()

	sessions.SetSession(c)
	tid := sessions.GetTid(c)
	if tid == "" {
		tid = "testtid"
	}
	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	logCtx.Debug("get content")

	action := rbmq.UserActionsNotify{
		Action: "content_get",
		Tid:    tid,
	}
	var err error
	defer func() {
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
		}
		if err := notifierService.ActionNotify(action); err != nil {
			logCtx.WithField("error", err.Error()).Error("notify user action")
		} else {
		}
		sessions.RemoveTid(c)
	}()
	logCtx.Debug(c.Request.Header)

	campaignHash := getCampaignHash(c)
	if len(campaignHash) != cnf.Service.CampaignHashLength {
		m.CampaignHashWrong.Inc()

		logCtx.WithFields(log.Fields{
			"campaignHash": campaignHash,
			"length":       len(campaignHash),
		}).Error("Length is too small")

		err := errors.New("Wrong campaign length")
		c.Error(err)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg, err := gatherInfo(tid, c)
	if err != nil {
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignHash = campaignHash
	action.Msisdn = msg.Msisdn
	logCtx = logCtx.WithField("msisdn", msg.Msisdn)
	logCtx.WithFields(log.Fields{}).Debug("gathered info, get content id..")

	contentProperties := &content_service.ContentSentProperties{}
	contentProperties, err = content.Get(content_service.GetUrlByCampaignHashParams{
		Msisdn:       msg.Msisdn,
		Tid:          tid,
		CampaignHash: campaignHash,
		CountryCode:  msg.CountryCode,
		OperatorCode: msg.OperatorCode,
		Publisher:    sessions.GetFromSession("publisher", c),
		Pixel:        sessions.GetFromSession("pixel", c),
	})
	if err != nil {
		m.ContentdRPCDialError.Inc()
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("content.Get: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("contentClient.Get")
		c.Error(err)
		msg.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		logCtx.Fatal("contentd fatal: trying to free all resources")
		return
	}
	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()

		err = fmt.Errorf("contentClient.Get: %s", contentProperties.Error)
		logCtx.WithField("error", contentProperties.Error).Error("contentClient.Get")
		err = errors.New(contentProperties.Error)
		c.Error(err)
		msg.Error = contentProperties.Error
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{
		"contentId": contentProperties.ContentId,
		"path":      contentProperties.ContentPath,
	}).Debug("contentd response")

	msg.CampaignId = contentProperties.CampaignId
	msg.ContentId = contentProperties.ContentId
	msg.ServiceId = contentProperties.ServiceId

	action.CampaignId = contentProperties.CampaignId

	// todo one time url-s
	err = utils.ServeAttachment(
		cnf.Server.Path+"uploaded_content/"+contentProperties.ContentPath,
		contentProperties.ContentName,
		c,
		logCtx,
	)
	if err != nil {
		m.ContentDeliveryErrors.Inc()
		err := fmt.Errorf("serveContentFile: %s", err.Error())
		logCtx.WithField("error", err.Error()).Error("serveContentFile")
		c.Error(err)
		msg.Error = err.Error()
		action.Error = err.Error()
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	logCtx.WithFields(log.Fields{}).Debug("served file ok")
	m.ContentGetSuccess.Inc()
	m.Success.Inc()
}

func getCampaignHash(c *gin.Context) string {
	campaignHash := c.Params.ByName("campaign_hash")
	if len(campaignHash) == cnf.Service.CampaignHashLength {
		return campaignHash
	}

	if campaignHash == "" {
		m.CampaignHashWrong.Inc()
	}
	return campaignHash
}

func AddCampaignHandler(r *gin.Engine) {
	log.WithField("route", "lp").Info("adding lp route")
	rg := r.Group("/lp/:campaign_link")
	rg.Use(AccessHandler)
	rg.Use(NotifyAccessCampaignHandler)
	rg.GET("", serveCampaigns)
}

func serveCampaigns(c *gin.Context) {
	campaignLink := c.Params.ByName("campaign_link")

	// important, do not use campaign from this operation
	// bcz we need to inc counter to process ratio
	if _, ok := campaignByLink[campaignLink]; !ok {
		m.PageNotFoundError.Inc()

		log.WithFields(log.Fields{
			"campaignLink": campaignLink,
			"error":        "not found",
		}).Error("cannot get campaign by link")

		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	m.CampaignAccess.Inc()
	m.Success.Inc()

	campaignByLink[campaignLink].Serve(c)

	if campaignByLink[campaignLink].CanAutoClick {
		r, err := startNewSubscription(c, campaignByLink[campaignLink].Hash)
		if err == nil {
			log.WithFields(log.Fields{
				"tid":        r.Tid,
				"link":       campaignLink,
				"hash":       campaignByLink[campaignLink].Hash,
				"msisdn":     r.Msisdn,
				"campaignid": campaignByLink[campaignLink].Id,
			}).Info("added new subscritpion due to ratio")
		}

	}
}

// on each access page
func NotifyAccessCampaignHandler(c *gin.Context) {
	sessions.SetSession(c)
	tid := sessions.GetTid(c)

	logCtx := log.WithFields(log.Fields{
		"tid": tid,
	})
	msg, err := gatherInfo(tid, c)
	if err != nil {
		logCtx.WithFields(log.Fields{
			"gatherInfo":     err.Error(),
			"accessCampaign": msg,
		}).Debug("gather access campaign")
	}

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]
	campaign, ok := campaignByLink[campaignLink]
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    tid,
		Msisdn: msg.Msisdn,
	}
	if !ok {
		log.WithFields(log.Fields{
			"path": campaignLink,
		}).Error("campaign is unknown")
	} else {
		msg.CampaignId = campaign.Id
		action.CampaignId = campaign.Id

		msg.CampaignHash = campaign.Hash
	}

	begin := time.Now()

	if err := notifierService.ActionNotify(action); err != nil {
		logCtx.WithFields(log.Fields{
			"error":  err.Error(),
			"action": action,
		}).Error("error notify user action")
	} else {
		logCtx.WithFields(log.Fields{
			"action": action,
			"took":   time.Since(begin),
		}).Info("done notify user action")
	}
	if err := notifierService.AccessCampaignNotify(msg); err != nil {
		logCtx.WithField("error", err.Error()).Error("notify access campaign")
	} else {
		logCtx.WithFields(log.Fields{}).Info("done notify access campaign")
	}
}

// just log and count all requests
func AccessHandler(c *gin.Context) {
	m.Overall.Inc()
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
