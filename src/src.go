package src

import (
	"errors"
	"runtime"

	log "github.com/Sirupsen/logrus"
	"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/utils/metrics"
)

var e *gin.Engine
var conf config.AppConfig

func RunServer() {
	conf = config.LoadConfig()
	m.Init(conf.MetricInstancePrefix, conf.AppName)

	handlers.Init(conf)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	e = gin.New()
	e.LoadHTMLGlob(conf.Server.Path + "campaign/**/*")
	e.GET("/updateTemplates", updateTemplates)

	sessions.Init(conf.Server.Sessions, e)

	handlers.AddCampaignHandler(e)
	metrics.AddHandler(e)

	rg := e.Group("/campaign/:campaign_hash")
	rg.GET("", handlers.AccessHandler, handlers.HandlePull)
	rg.GET("/contentget", handlers.AccessHandler, handlers.ContentGet)

	e.Static("/static/", conf.Server.Path+"/static/")
	e.StaticFile("/favicon.ico", conf.Server.Path+"/favicon.ico")
	e.StaticFile("/robots.txt", conf.Server.Path+"/robots.txt")

	e.NoRoute(notFound)

	e.RedirectTrailingSlash = true

	endless.ListenAndServe(":"+conf.Server.Port, e)
	//e.Run(":" + conf.Server.Port)
}

func notFound(c *gin.Context) {
	c.Error(errors.New("Not found"))
	m.PageNotFoundError.Inc()
}

func updateTemplates(c *gin.Context) {
	path := conf.Server.Path + "campaign/**/*"
	log.Debug("update templates path: " + path)
	e.LoadHTMLGlob(path)
	handlers.UpdateCampaignByLink()
	c.JSON(200, struct{}{})
}
