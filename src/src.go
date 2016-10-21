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
)

func RunServer() {
	appConfig := config.LoadConfig()
	operator.Init(appConfig.Operator)
	metrics.Init()
	handlers.Init(appConfig)
	newrelic.Init(appConfig.NewRelic)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()

	operator.AddCQRHandlers(r)

	r.Use(metrics.MetricHandler)
	r.GET("/campaign/:campaign_hash", handlers.HandlePull)

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
