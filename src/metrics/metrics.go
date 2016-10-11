package metrics

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/expvar"

	"github.com/vostrok/rabbit"
)

var M AppMetrics

type AppMetrics struct {
	RequestsOverall LocationMetric
	NotFound        metrics.Counter
	RBMQMetrics     rabbit.RBMQMetrics
}

func Init() AppMetrics {
	M = AppMetrics{
		RequestsOverall: NewLocationMetric("requests_overall"),
		NotFound:        expvar.NewCounter("errors_404"),
		RBMQMetrics: rabbit.RBMQMetrics{
			RbmqConnAttempt:     expvar.NewCounter("rbmq_conn_attempts_count"),
			RbmqSessionRequests: expvar.NewCounter("rbmq_conn_reconnects_count"),
			RbmqPublishErrs:     expvar.NewCounter("rbmq_conn_errs_pub_count"),
			RbmqConnected:       expvar.NewGauge("rbmq_conn_status"),
			PendingBuffer:       expvar.NewGauge("rbmq_buffer_pending_gauge"),
			ReadingBuffer:       expvar.NewGauge("rbmq_buffer_reading_gauge"),
		},
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

var quantiles = []int{50, 90, 95, 99}

type MethodTimeMetric struct {
	th       metrics.TimeHistogram
	overtime metrics.Counter
}

func (m MethodTimeMetric) CatchOverTime(dur time.Duration, max time.Duration) {
	if dur > max {
		m.overtime.Add(1)
	}
	m.th.Observe(dur)
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
		metrics.NewTimeHistogram(time.Millisecond,
			expvar.NewHistogram("duration_ms_"+name, 0, 10000, 3, quantiles...)),
		expvar.NewCounter("overtime_" + name),
	}
	lm.Count = expvar.NewCounter("access_" + name)
	lm.Errors = expvar.NewCounter("errors_" + name)
	return lm
}
