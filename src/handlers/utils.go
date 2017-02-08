package handlers

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/dispatcherd/src/config"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/rbmq"
	inmem_client "github.com/vostrok/inmem/rpcclient"
	inmem_service "github.com/vostrok/inmem/service"
	redirect_client "github.com/vostrok/partners/rpcclient"
	redirect_service "github.com/vostrok/partners/service"
)

var cnf config.AppConfig

var notifierService rbmq.Notifier

var campaignByLink map[string]*inmem_service.Campaign
var campaignByHash map[string]inmem_service.Campaign

func Init(conf config.AppConfig) {
	log.SetLevel(log.DebugLevel)

	cnf = conf

	content.Init(conf.ContentClient)
	if err := inmem_client.Init(conf.InMemConfig); err != nil {
		log.Fatal("cannot init inmem client")
	}
	if err := redirect_client.Init(conf.RedirectConfig); err != nil {
		log.Fatal("cannot redirect client")
	}
	UpdateCampaigns()
	notifierService = rbmq.NewNotifierService(conf.Notifier)
}

// update campaign list
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
	}
	hit.DestinationId = dst.DestinationId
	hit.PartnerId = dst.PartnerId
	hit.Destination = dst.Destination
	hit.PricePerHit = dst.PricePerHit
	hit.CountryCode = dst.CountryCode
	hit.OperatorCode = dst.OperatorCode
	m.TrafficRedirectSuccess.Inc()
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
