package handlers

import (
	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
)

func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}
	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	e.Group("/api/:campaign_link").GET("", AccessHandler, initiateSubscription)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}
