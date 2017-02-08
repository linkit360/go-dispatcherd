package src

import (
	"errors"
	"net/http"
	"runtime"

	log "github.com/Sirupsen/logrus"
	//"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/sessions"
	"github.com/vostrok/utils/metrics"
)

var conf config.AppConfig

func RunServer() {
	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	conf = config.LoadConfig()
	m.Init(conf.AppName)

	handlers.Init(conf)

	e := gin.New()

	sessions.Init(conf.Server.Sessions, e)
	metrics.AddHandler(e)

	rg := e.Group("/campaign/:campaign_hash")
	handlers.AddCampaignHandler(e, rg)
	handlers.AddContentHandlers(e, rg)
	handlers.AddBeelineHandlers(rg)

	e.Static("/static/", conf.Server.Path+"/static/")
	e.StaticFile("/favicon.ico", conf.Server.Path+"/favicon.ico")
	e.StaticFile("/robots.txt", conf.Server.Path+"/robots.txt")
	e.NoRoute(notFound)

	e.RedirectTrailingSlash = true
	e.Run(":" + conf.Server.Port)
	//endless.ListenAndServe(":"+conf.Server.Port, e)
}

func notFound(c *gin.Context) {
	c.Error(errors.New("Not found"))
	m.PageNotFoundError.Inc()
	http.Redirect(c.Writer, c.Request, conf.Service.NotFoundRedirectUrl, 303)
}
