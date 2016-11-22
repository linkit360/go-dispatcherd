package metrics

import (
	m "github.com/vostrok/utils/metrics"
)

var (
	Overall               m.Gauge
	Access                m.Gauge
	Agree                 m.Gauge
	AgreeSuccess          m.Gauge
	ContentGetSuccess     m.Gauge
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
	ContentGetSuccess = newGaugeHttpRequests("content_get", "pressed the button 'get content' and successfully processed")

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
}
