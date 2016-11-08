package src

import (
	"errors"
	"runtime"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/config"
	"github.com/vostrok/dispatcherd/src/handlers"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/operator"
	"github.com/vostrok/dispatcherd/src/sessions"
)

func RunServer() {
	appConfig := config.LoadConfig()
	operator.Init(appConfig.Operator, appConfig.Db)
	campaigns.Init(appConfig.Server.Path, appConfig.Db)

	metrics.Init()
	handlers.Init(appConfig)

	nuCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(nuCPU)
	log.WithField("CPUCount", nuCPU)

	r := gin.New()
	sessions.Init(appConfig.Server.Sessions, r)

	r.Use(AccessHandler)

	handlers.AddCampaignHandlers(r)
	handlers.AddCQRHandler(r)

	r.GET("/campaign/:campaign_hash", handlers.HandlePull)

	rg := r.Group("/metrics")
	rg.GET("", gin.WrapH(prometheus.Handler()))

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
	metrics.M.PageNotFoundError.Add(1)
}

func AccessHandler(c *gin.Context) {
	begin := time.Now()
	c.Next()

	responseTime := time.Since(begin)
	tid := sessions.GetTid(c)

	if len(c.Errors) > 0 {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"error":  c.Errors.String(),
			"since":  responseTime,
		}).Error(c.Errors.String())
	} else {
		log.WithFields(log.Fields{
			"tid":    tid,
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
			"req":    c.Request.URL.RawQuery,
			"since":  responseTime,
		}).Info("access")
	}
	c.Header("X-Response-Time", responseTime.String())
}
