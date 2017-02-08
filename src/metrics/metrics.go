package metrics

import (
	m "github.com/vostrok/utils/metrics"
	"time"
)

var (
	Incoming               m.Gauge
	Errors                 m.Gauge
	Success                m.Gauge
	Access                 m.Gauge
	Agree                  m.Gauge
	AgreeSuccess           m.Gauge
	Redirected             m.Gauge
	Rejected               m.Gauge
	CampaignAccess         m.Gauge
	RandomContentGet       m.Gauge
	UniqueUrlGet           m.Gauge
	ContentGetSuccess      m.Gauge
	TrafficRedirectSuccess m.Gauge

	PageNotFoundError          m.Gauge
	CampaignHashWrong          m.Gauge
	ContentDeliveryErrors      m.Gauge
	IPNotFoundError            m.Gauge
	MsisdnNotFoundError        m.Gauge
	NotSupported               m.Gauge
	OperatorNameError          m.Gauge
	NotifyNewSubscriptionError m.Gauge
)

func newGaugeCommon(name, help string) m.Gauge {
	return m.NewGauge("", appName, name, ""+help)
}
func newGaugeGatherErrors(name, help string) m.Gauge {
	return m.NewGauge("", appName, name, ""+help)
}

var appName string

func Init(name string) {
	appName = name

	Success = m.NewGauge("", "", "success", "success overall")
	Errors = m.NewGauge("", "", "errors", "errors overall")
	Incoming = newGaugeCommon("incoming", "overall")
	Agree = newGaugeCommon("agreed", "pressed the button 'agree'")
	Redirected = newGaugeCommon("redirected", "redirected due to rejected")
	AgreeSuccess = newGaugeCommon("agree_success", "pressed the button 'agree' and successfully processed")
	CampaignAccess = newGaugeCommon("campaign_access", "campaign access success")
	ContentGetSuccess = newGaugeCommon("content_get", "pressed the button 'get content' and successfully processed")
	RandomContentGet = newGaugeCommon("random_content_get", "get random content from the url /u/get")
	UniqueUrlGet = newGaugeCommon("unique_url_get", "get the uniq url from the sms")
	TrafficRedirectSuccess = newGaugeCommon("traffice_redirect_success", "traffic redirect success")

	PageNotFoundError = newGaugeCommon("error404", "404 requests")
	Rejected = newGaugeCommon("truly_rejected", "no more campaigns for msisdn - rejected")
	CampaignHashWrong = newGaugeCommon("campaign_hash_wrong", "campaign hash wrong")
	ContentDeliveryErrors = newGaugeCommon("serve_errors", "content delivery errors")

	IPNotFoundError = newGaugeGatherErrors("ip_not_found", "ip not found")
	MsisdnNotFoundError = newGaugeGatherErrors("msisdn_not_found", "msisdn not found")
	NotSupported = newGaugeGatherErrors("not_supported", " operator is not supported")
	OperatorNameError = newGaugeGatherErrors("operator_name", "cannot determine operator name by code")
	NotifyNewSubscriptionError = newGaugeCommon("notify_new_subscription_error", "cannot notify new subscription")

	go func() {
		for range time.Tick(time.Minute) {
			Success.Update()
			Errors.Update()
			Incoming.Update()
			Agree.Update()
			Redirected.Update()
			AgreeSuccess.Update()
			CampaignAccess.Update()
			ContentGetSuccess.Update()
			RandomContentGet.Update()
			UniqueUrlGet.Update()
			TrafficRedirectSuccess.Update()

			PageNotFoundError.Update()
			Rejected.Update()
			CampaignHashWrong.Update()
			ContentDeliveryErrors.Update()
			IPNotFoundError.Update()
			MsisdnNotFoundError.Update()
			NotSupported.Update()
			OperatorNameError.Update()
			NotifyNewSubscriptionError.Update()
		}
	}()
}
