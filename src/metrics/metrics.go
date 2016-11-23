package metrics

import (
	m "github.com/vostrok/utils/metrics"
)

var (
	Overall           m.Gauge
	Errors            m.Gauge
	Success           m.Gauge
	Access            m.Gauge
	Agree             m.Gauge
	AgreeSuccess      m.Gauge
	CampaignAccess    m.Gauge
	ContentGetSuccess m.Gauge

	PageNotFoundError     m.Gauge
	CampaignHashWrong     m.Gauge
	ContentDeliveryErrors m.Gauge
	ContentdRPCDialError  m.Gauge
	IPNotFoundError       m.Gauge
	MsisdnNotFoundError   m.Gauge
	NotSupported          m.Gauge
	GetInfoByMsisdnError  m.Gauge
	OperatorNameError     m.Gauge
)

func newGaugeCommon(name, help string) m.Gauge {
	return m.NewGauge("", "", name, ""+help)
}
func newGaugeGatherErrors(name, help string) m.Gauge {
	return m.NewGauge("", "", name, ""+help)
}

func Init(appName string) {
	m.Init(appName)

	Success = m.NewGauge("", "", "success", "success overall")
	Errors = m.NewGauge("", "", "errors", "errors overall")
	Overall = newGaugeCommon("overall", "overall")
	Agree = newGaugeCommon("agreed", "pressed the button 'agree'")
	AgreeSuccess = newGaugeCommon("agree_success", "pressed the button 'agree' and successfully processed")
	CampaignAccess = newGaugeCommon("campaign_access", "campaign access success")
	ContentGetSuccess = newGaugeCommon("content_get", "pressed the button 'get content' and successfully processed")

	PageNotFoundError = newGaugeCommon("error404", "404 requests")
	CampaignHashWrong = newGaugeCommon("campaign_hash_wrong", "campaign hash wrong")
	ContentDeliveryErrors = newGaugeCommon("serve_errors", "content delivery errors")
	ContentdRPCDialError = newGaugeCommon("contentd_rpc_errors", "number of connect errors ")

	IPNotFoundError = newGaugeGatherErrors("ip_not_found", "ip not found")
	MsisdnNotFoundError = newGaugeGatherErrors("msisdn_not_found", "msisdn not found")
	NotSupported = newGaugeGatherErrors("not_supported", " operator is not supported")
	GetInfoByMsisdnError = newGaugeGatherErrors("info_by_msisdn", "cannot find info by msisdn")
	OperatorNameError = newGaugeGatherErrors("operator_name", "cannot determine operator name by code")
}
