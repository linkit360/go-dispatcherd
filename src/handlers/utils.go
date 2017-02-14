package handlers

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content_client "github.com/vostrok/contentd/rpcclient"
	content_service "github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/config"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
	redirect_client "github.com/vostrok/partners/rpcclient"
	redirect_service "github.com/vostrok/partners/service"
)

// file for global variables,
// initialisation
// common functions

var cnf config.AppConfig
var e *gin.Engine
var notifierService rbmq.Notifier

var campaignByLink map[string]*inmem_service.Campaign
var campaignByHash map[string]inmem_service.Campaign

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf

	if err := content_client.Init(conf.ContentClient); err != nil {
		log.Fatal("cannot init contentd client")
	}
	if err := inmem_client.Init(conf.InMemConfig); err != nil {
		log.Fatal("cannot init inmem client")
	}
	if err := redirect_client.Init(conf.RedirectConfig); err != nil {
		log.Fatal("cannot redirect client")
	}
	UpdateCampaigns()
	notifierService = rbmq.NewNotifierService(conf.Notifier)
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
// when campaign changes, CQR request comes to inmem service
// and from inmem service it goes to dispatcher
func UpdateCampaigns() error {
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
	for key, _ := range campaignByHash {
		delete(campaignByHash, key)
	}
	campaignByLink = make(map[string]*inmem_service.Campaign, len(campaigns))
	campaignByHash = make(map[string]inmem_service.Campaign, len(campaigns))
	for _, campaign := range campaigns {
		camp := campaign
		campaignByLink[campaign.Link] = &camp
		campaignByHash[campaign.Hash] = camp
	}
	log.WithFields(log.Fields{
		"len": len(campaigns),
		"c":   fmt.Sprintf("%#v", campaignByLink),
	}).Info("campaigns updated")
	return nil
}

type EventNotify struct {
	EventName string                          `json:"event_name,omitempty"`
	EventData redirect_service.DestinationHit `json:"event_data,omitempty"`
}

// traffic redirect
func trafficRedirect(r rbmq.AccessCampaignNotify, c *gin.Context) {
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

	inmem_client.IncRedirectStatCount(dst.DestinationId)

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
func redirect(msg rbmq.AccessCampaignNotify) (campaign inmem_service.Campaign, err error) {
	if !cnf.Service.Rejected.CampaignRedirectEnabled {
		log.WithFields(log.Fields{
			"tid": msg.Tid,
		}).Debug("redirect off")
		campaign.Id = msg.CampaignId
		return
	}

	// if nextCampaignId == msg.CampaignId then it's not rejected msisdn
	campaign.Id, err = inmem_client.GetMsisdnCampaignCache(msg.CampaignId, msg.Msisdn)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetMsisdnCampaignCache: %s", err.Error())
		log.WithFields(log.Fields{
			"tid":   msg.Tid,
			"error": err.Error(),
		}).Debug("redirect check faieled")
		return
	}

	if campaign.Id == msg.CampaignId {
		log.WithFields(log.Fields{
			"tid": msg.Tid,
		}).Debug("no redirect: ok")
		return
	}
	// no more campaigns
	if campaign.Id == 0 {
		m.Rejected.Inc()

		log.WithFields(log.Fields{
			"tid":      msg.Tid,
			"msisdn":   msg.Msisdn,
			"campaign": msg.CampaignId,
		}).Debug("redirect")
		return
	}

	campaign, err = inmem_client.GetCampaignById(campaign.Id)
	if err != nil {
		err = fmt.Errorf("inmem_client.GetCampaignById: %s", err.Error())

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
		"campaign":  msg.CampaignId,
		"2campaign": campaign.Id,
	}).Debug("redirect")
	return
}

// generate unique url for user
// the unique url creation is inside contentd service
func generateUniqueUrl(r rbmq.AccessCampaignNotify) (url string, err error) {
	logCtx := log.WithFields(log.Fields{
		"tid": r.Tid,
	})
	service, err := inmem_client.GetServiceById(r.ServiceId)
	if err != nil {
		m.UnknownService.Inc()

		err = fmt.Errorf("inmem_client.GetServiceById: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceId,
			"error":     err.Error(),
		}).Error("cannot get service by id")
		return
	}
	contentProperties, err := content_client.GetUniqueUrl(content_service.GetContentParams{
		Msisdn:       r.Msisdn,
		Tid:          r.Tid,
		ServiceId:    r.ServiceId,
		CampaignId:   r.CampaignId,
		OperatorCode: r.OperatorCode,
		CountryCode:  r.CountryCode,
	})

	if contentProperties.Error != "" {
		m.ContentDeliveryErrors.Inc()
		err = fmt.Errorf("content_client.GetUniqueUrl: %s", contentProperties.Error)
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceId,
			"error":     err.Error(),
		}).Error("contentd internal error")
		return
	}
	if err != nil {
		err = fmt.Errorf("content_client.GetUniqueUrl: %s", err.Error())
		logCtx.WithFields(log.Fields{
			"serviceId": r.ServiceId,
			"error":     err.Error(),
		}).Error("cannot get unique content url")
		return
	}
	url = fmt.Sprintf(service.SendContentTextTemplate, cnf.Server.Url+"/u/"+contentProperties.UniqueUrl)
	return
}
