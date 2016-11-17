package src

import (
	"errors"
	"runtime"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/metrics"
)

func RunServer() {
	appConfig := config.LoadConfig()
	m.Init(appConfig.Name)

	operator.Init(appConfig.Operator, appConfig.Db)
	campaigns.Init(appConfig.Server.Path, appConfig.Db)

	handlers.Init(appConfig)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()
	sessions.Init(appConfig.Server.Sessions, r)

	handlers.AddCampaignHandlers(r)
	handlers.AddCampaignHandler(r)
	handlers.AddCQRHandler(r)

	metrics.AddHandler(r)

	r.GET("/campaign/:campaign_hash", handlers.AccessHandler, handlers.HandlePull)
	r.Static("/static/", appConfig.Server.Path+"/static/")
	r.StaticFile("/favicon.ico", appConfig.Server.Path+"/favicon.ico")
	r.StaticFile("/robots.txt", appConfig.Server.Path+"/robots.txt")

	r.NoRoute(notFound)

	r.RedirectTrailingSlash = true
	r.RedirectFixedPath = true

	r.Run(":" + appConfig.Server.Port)
}

func notFound(c *gin.Context) {
	c.Error(errors.New("Not found"))
	m.PageNotFoundError.Inc()
}
