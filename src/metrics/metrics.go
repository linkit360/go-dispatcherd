package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	m "github.com/vostrok/metrics"
)

var (
	LoadCampaignError     prometheus.Gauge
	Overall               m.Gauge
	Access                m.Gauge
	Agree                 m.Gauge
	AgreeSuccess          m.Gauge
	OperatorNameError     m.Gauge
	Errors                m.Gauge
	PageNotFoundError     m.Gauge
	IPNotFoundError       m.Gauge
	MsisdnNotFoundError   m.Gauge
	GetInfoByMsisdn       m.Gauge
	NotSupported          m.Gauge
	CampaignHashWrong     m.Gauge
	ContentDeliveryErrors m.Gauge
	ContentdRPCDialError  m.Gauge
	//OperatorNotApplicable m.Gauge
	//OperatorNotEnabled    m.Gauge
)

func newGaugeHttpRequests(name, help string) m.Gauge {
	return m.NewGauge("", "http_requests", name, "http_requests "+help)
}
func newGaugeIncomingTraffic(name, help string) m.Gauge {
	return m.NewGauge("", "incoming", name, "incoming "+help)
}
func newGaugeContentd(name, help string) m.Gauge {
	return m.NewGauge("", "contentd", name, "contentd "+help)
}

func Init(appName string) {

	m.Init(appName)
	Overall = newGaugeHttpRequests("overall", "overall")
	Access = newGaugeHttpRequests("access", "opened static")
	Agree = newGaugeHttpRequests("agreed", "pressed the button 'agree'")
	AgreeSuccess = newGaugeHttpRequests("agree_success", "pressed the button 'agree' and successfully processed")
	Errors = m.NewGauge("", "", "errors", "http_requests errors")
	CampaignHashWrong = newGaugeHttpRequests("campaign_hash_wrong", "campaign hash wrong")

	IPNotFoundError = newGaugeIncomingTraffic("ip_not_found", "ip not found")
	MsisdnNotFoundError = newGaugeIncomingTraffic("msisdn_not_found", "msisdn not found")
	NotSupported = newGaugeIncomingTraffic("not_supported", " operator is not supported")
	GetInfoByMsisdn = newGaugeIncomingTraffic("info_by_msisdn", "cannot find info by msisdn")
	OperatorNameError = newGaugeIncomingTraffic("operator_name", "cannot determine operator name by code")

	ContentDeliveryErrors = newGaugeHttpRequests("serve_errors", "content delivery errors")
	PageNotFoundError = newGaugeHttpRequests("error404", "404 requests")
	ContentdRPCDialError = newGaugeContentd("connect_errors", "number of connect errors ")
	LoadCampaignError = m.PrometheusGauge(
		"",
		"campaign",
		"load_error",
		"Load campaign HTML error",
	)
	//OperatorNotApplicable = m.NewGauge("", "", "not_applicable", "operator not applicable ")
	//OperatorNotEnabled = m.NewGauge("", "", "not_enabled", "operator not enabled ")

	go func() {
		for range time.Tick(time.Minute) {
			Overall.Update()
			Access.Update()
			Agree.Update()
			AgreeSuccess.Update()
			OperatorNameError.Update()
			Errors.Update()
			PageNotFoundError.Update()
			IPNotFoundError.Update()
			MsisdnNotFoundError.Update()
			GetInfoByMsisdn.Update()
			NotSupported.Update()
			CampaignHashWrong.Update()
			ContentDeliveryErrors.Update()
			ContentdRPCDialError.Update()
			//OperatorNotEnabled.Update()
			//OperatorNotApplicable.Update()
		}
	}()
}
