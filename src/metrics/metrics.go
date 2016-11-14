package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	m "github.com/vostrok/metrics"
)

var (
	Overall               prometheus.Counter
	Access                prometheus.Counter
	Agree                 prometheus.Counter
	Errors                prometheus.Counter
	PageNotFoundError     prometheus.Counter
	IPNotFoundError       prometheus.Counter
	MsisdnNotFoundError   prometheus.Counter
	GetInfoByMsisdn       prometheus.Counter
	NotSupported          prometheus.Counter
	CampaignHashWrong     prometheus.Counter
	ContentDeliveryErrors prometheus.Counter
	ContentdRPCDialError  prometheus.Counter
	LoadCampaignError     prometheus.Gauge
)

func newCounterHttpRequests(name, help string) prometheus.Counter {
	return m.NewCounter("http_requests_"+name, "http_requests "+help)
}
func newCounterIncomingTraffic(name, help string) prometheus.Counter {
	return m.NewCounter("incoming_"+name, "incoming "+help)
}
func newCounterContentd(name, help string) prometheus.Counter {
	return m.NewCounter("contentd_"+name, "contentd "+help)
}

func Init(appName string) {

	m.Init(appName)
	Overall = newCounterHttpRequests("overall", "overall")
	Access = newCounterHttpRequests("access", "opened static")
	Agree = newCounterHttpRequests("agreed", "pressed the button 'agree'")
	Errors = newCounterHttpRequests("error", "error")
	CampaignHashWrong = newCounterHttpRequests("campaign_hash_wrong", "campaign hash wrong")

	IPNotFoundError = newCounterIncomingTraffic("ip_not_found", "ip not found")
	MsisdnNotFoundError = newCounterIncomingTraffic("msisdn_not_found", "msisdn not found")
	NotSupported = newCounterIncomingTraffic("not_supported", " operator is not supported")
	GetInfoByMsisdn = newCounterIncomingTraffic("info_by_msisdn", "cannot find info by msisdn")

	ContentDeliveryErrors = newCounterHttpRequests("serve_errors", "content delivery errors")
	PageNotFoundError = newCounterHttpRequests("error404", "404 requests")
	ContentdRPCDialError = newCounterContentd("connect_errors", "number of connect errors ")
	LoadCampaignError = m.PrometheusGauge(
		"",
		"campaign",
		"load_error",
		"Load campaign HTML error",
	)
}
