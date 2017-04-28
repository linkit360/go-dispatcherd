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
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}
