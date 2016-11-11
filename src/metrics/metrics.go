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
	IPNotFoundError       m.Gauge
	MsisdnNotFoundError   m.Gauge
	NotSupported          m.Gauge
	CampaignHashWrong     m.Gauge
	ContentDeliveryErrors m.Gauge
	ContentdRPCDialError  m.Gauge
	LoadCampaignError     prometheus.Gauge
)

func newGaugeHttpRequests(name, help string) m.Gauge {
	return m.NewGaugeMetric("http_requests", name, "http_requests "+help)
}
func newGaugeIncomingTraffic(name, help string) m.Gauge {
	return m.NewGaugeMetric("incoming", name, "incoming "+help)
}
func newGaugeContentd(name, help string) m.Gauge {
	return m.NewGaugeMetric("contentd", name, "contentd "+help)
}
func Init(appName string) {

	m.Init(appName)
	Overall = newGaugeHttpRequests("overall", "overall")
	Access = newGaugeHttpRequests("access", "opened static")
	Agree = newGaugeHttpRequests("agreed", "pressed the button 'agree'")
	Errors = newGaugeHttpRequests("error", "error")

	CampaignHashWrong = newGaugeHttpRequests("campaign_hash_wrong", "campaign hash wrong")
	IPNotFoundError = newGaugeIncomingTraffic("ip_not_found", "ip not found")
	MsisdnNotFoundError = newGaugeIncomingTraffic("msisdn_not_found", "msisdn not found")
	NotSupported = newGaugeIncomingTraffic("not_supported", " operator is not supported")
	ContentDeliveryErrors = newGaugeHttpRequests("serve_errors", "content delivery errors")
	PageNotFoundError = newGaugeHttpRequests("error404", "404 requests")
	ContentdRPCDialError = newGaugeContentd("connect_errors", "number of connect errors ")
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
			CampaignHashWrong.Update()
			IPNotFoundError.Update()
			MsisdnNotFoundError.Update()
			NotSupported.Update()
			PageNotFoundError.Update()
			ContentDeliveryErrors.Update()
			ContentdRPCDialError.Update()
		}
	}()
}
