package handlers

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
)

func AddMobilinkHandlers(e *gin.Engine) {
	if !cnf.Service.LandingPages.Mobilink.Enabled {
		return
	}
	e.Group("/lp/:campaign_link", AccessHandler).GET("", serveCampaigns)
	e.Group("/campaign/:campaign_link").GET("", AccessHandler, initiateSubscription)
	log.WithFields(log.Fields{}).Debug("mobilink handlers init")
}
