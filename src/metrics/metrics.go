package metrics

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/expvar"
)

var M AppMetrics

type AppMetrics struct {
	RequestsOverall      LocationMetric
	ContentDeliveryError metrics.Counter
	MsisdnNotFound       metrics.Gauge
	NotFound             metrics.Counter
}

func Init() AppMetrics {
	M = AppMetrics{
		RequestsOverall:      NewLocationMetric("requests_overall"),
		ContentDeliveryError: expvar.NewCounter("error_content_delivery"),
		MsisdnNotFound:       expvar.NewGauge("error_msisdn_not_found"),
		NotFound:             expvar.NewCounter("errors_404"),
	}
	return M
}

func MetricHandler(c *gin.Context) {
	begin := time.Now()
	c.Next()

	M.RequestsOverall.Time.CatchOverTime(time.Since(begin), time.Second)
	M.RequestsOverall.Count.Add(1)

	if len(c.Errors) > 0 {
		M.RequestsOverall.Errors.Add(1)
	}
}

type MethodTimeMetric struct {
	overtime metrics.Counter
}

func (m MethodTimeMetric) CatchOverTime(dur time.Duration, max time.Duration) {
	if dur > max {
		m.overtime.Add(1)
	}
}

type LocationMetric struct {
	Time   MethodTimeMetric
	Count  metrics.Counter
	Errors metrics.Counter
}

func NewLocationMetric(name string) (lm LocationMetric) {
	if name == "" {
		log.Fatal("locationMetric", "no name for location metric")
	}
	lm.Time = MethodTimeMetric{
		expvar.NewCounter("overtime_" + name),
	}
	lm.Count = expvar.NewCounter("access_" + name)
	lm.Errors = expvar.NewCounter("errors_" + name)
	return lm
}
