package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	m "github.com/vostrok/metrics"
)

var (
	Overall               m.Gauge
	Access                m.Gauge
	Agree                 m.Gauge
	Errors                m.Gauge
	PageNotFoundError     m.Gauge
	ContentDeliveryErrors m.Gauge
	ContentdRPCDialError  m.Gauge
	LoadCampaignError     prometheus.Gauge
)

func newGaugeHttpRequests(name, help string) m.Gauge {
	return m.NewCustomMetric("http_requests", name, "http_requests "+help)
}

func newGaugeContentd(name, help string) m.Gauge {
	return m.NewCustomMetric("contentd", name, "contentd "+help)
}
func Init(appName string) {

	m.Init(appName)
	Overall = newGaugeHttpRequests("overall", "rpm, overall")
	Access = newGaugeHttpRequests("access", "rpm, opened static")
	Agree = newGaugeHttpRequests("agreed", "rpm, pressed the button 'agree'")
	Errors = newGaugeHttpRequests("errors", "rpm, errors")
	ContentDeliveryErrors = newGaugeHttpRequests("serve_errors", "rpm, content delivery errors")
	PageNotFoundError = newGaugeHttpRequests("error404", "rpm, 404 requests")
	ContentdRPCDialError = newGaugeContentd("connect_errors", "rpm, number of connect errors ")
	LoadCampaignError = m.PrometheusGauge(
		"",
		"campaign",
		"load_error",
		"Load campaign HTML error",
	)
	go func() {
		// metrics in prometheus as for 15s (default)
		// so make for minute interval
		for range time.Tick(time.Minute) {
			Overall.Update()
			Access.Update()
			Agree.Update()
			Errors.Update()
			PageNotFoundError.Update()
			ContentDeliveryErrors.Update()
			ContentdRPCDialError.Update()
		}
	}()
}
