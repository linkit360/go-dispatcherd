package src

import (
	"errors"
	"runtime"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/expvar"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/newrelic"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

func RunServer() {
	appConfig := config.LoadConfig()
	operator.Init(appConfig.Operator)
	metrics.Init()
	handlers.Init(appConfig)
	newrelic.Init(appConfig.NewRelic)

	rbmq.NewNotifierService(appConfig.Notifier)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()

	operator.AddCQRHandlers(r)

	r.Use(metrics.MetricHandler)
	r.GET("/:subscription_hash", handlers.HandlePull)

	r.Static("/static/", appConfig.Subscriptions.StaticPath)
	r.StaticFile("/favicon.ico", appConfig.Subscriptions.StaticPath+"/favicon.ico")
	r.StaticFile("/robots.txt", appConfig.Subscriptions.StaticPath+"/robots.txt")

	rg := r.Group("/debug")
	rg.GET("/vars", expvar.Handler())

	r.NoRoute(notFound)

	r.RedirectTrailingSlash = true
	r.RedirectFixedPath = true

	newrelic.RecordInitApp()

	r.Run(":" + appConfig.Server.Port)
}

func notFound(c *gin.Context) {
	c.Error(errors.New("Not found"))
	metrics.M.NotFound.Add(1)
}
