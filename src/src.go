package src

import (
	"errors"
	"runtime"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/expvar"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/newrelic"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/sessions"
)

func RunServer() {
	appConfig := config.LoadConfig()
	operator.Init(appConfig.Operator, appConfig.Db)
	campaigns.Init(appConfig.Server.StaticPath, appConfig.Db)

	metrics.Init()
	handlers.Init(appConfig)
	newrelic.Init(appConfig.NewRelic)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()
	sessions.Init(appConfig.Server.Sessions, r)

	r.Use(AccessHandler)
	r.Use(sessions.AddSessionTidHandler)

	handlers.AddCQRHandler(r)

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

func AccessHandler(c *gin.Context) {
	begin := time.Now()
	c.Next()

	responseTime := time.Since(begin)

	if len(c.Errors) > 0 {
		log.WithFields(log.Fields{
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"error":  c.Errors.String(),
			"since":  responseTime,
		}).Error("Error")
	} else {
		log.WithFields(log.Fields{
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"since":  responseTime,
		}).Info("Access")
	}
	c.Header("X-Response-Time", responseTime.String())
}
