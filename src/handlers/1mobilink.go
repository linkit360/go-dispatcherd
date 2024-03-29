package handlers

import (
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}
	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	e.HEAD("/lp/:campaign_link/*filepath", ServeStatic)
	e.GET("/lp/:campaign_link/*filepath", ServeStatic)

	e.Group("/api/:campaign_link").GET("", AccessHandler, initiateSubscription)
	e.Group("/api/:campaign_link").GET("/", AccessHandler, initiateSubscription)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}
