package src

import (
	"errors"
	"net/http"
	"runtime"

	log "github.com/Sirupsen/logrus"
	//"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"

	"github.com/linkit360/go-dispatcherd/src/config"
	"github.com/linkit360/go-dispatcherd/src/handlers"
	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	"github.com/linkit360/go-utils/metrics"
)

var conf config.AppConfig

func RunServer() {
	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	conf = config.LoadConfig()
	m.Init(conf.AppName)

	e := gin.New()
	handlers.Init(conf, e)

	sessions.Init(conf.Server.Sessions, e)
	metrics.AddHandler(e)
	handlers.AddContentHandlers()

	rg := e.Group("/campaign/:campaign_hash")
	handlers.AddCampaignHandler(rg)
	handlers.AddBeelineHandlers(e)
	handlers.AddQRTechHandlers()

	e.Static("/static/", conf.Server.Path+"/static/")
	e.StaticFile("/favicon.ico", conf.Server.Path+"/favicon.ico")
	e.StaticFile("/robots.txt", conf.Server.Path+"/robots.txt")
	e.NoRoute(handlers.AccessHandler, notFound)

	e.RedirectTrailingSlash = true
	e.Run(":" + conf.Server.Port)
	//endless.ListenAndServe(":"+conf.Server.Port, e)
}

func notFound(c *gin.Context) {
	c.Error(errors.New("Not found"))
	m.PageNotFoundError.Inc()
	http.Redirect(c.Writer, c.Request, conf.Service.NotFoundRedirectUrl, 303)
}
