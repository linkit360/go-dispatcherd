package src

import (
	"runtime"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/expvar"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/newrelic"
	"github.com/vostrok/dispatcherd/src/rbmq"
)

func RunServer() {
	appConfig := config.LoadConfig()
	metrics.Init()
	handlers.Init(appConfig)
	newrelic.Init(appConfig.NewRelic)
	rbmq.NewNotifierService(appConfig.Server.RBMQQueueName, appConfig.RBMQ)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()

	r.Use(metrics.MetricHandler)
	r.GET("/:subscription_hash", handlers.HandleSubscription)

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

func notFound(r *gin.Context) {
	metrics.M.NotFound.Add(1)
}
