package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	m "github.com/vostrok/metrics"
)

var (
	Overall               int64
	Acess                 int64
	Agree                 int64
	Errors                int64
	PageNotFoundError     int64
	ContentDeliveryErrors int64
	ContentdRPCDialError  int64
)
var (
	rpmOverall               prometheus.Gauge
	rpmAccess                prometheus.Gauge
	rpmAgree                 prometheus.Gauge
	rpmErrors                prometheus.Gauge
	rpmPageNotFoundError     prometheus.Gauge
	rpmContentDeliveryErrors prometheus.Gauge
	rpmContentdRPCDialError  prometheus.Gauge
	LoadCampaignError        prometheus.Gauge
)

func Init(appName string) {

	m.Init(appName)
	rpmOverall = m.NewGauge(
		"",
		"",
		"http_requests_rpm",
		"rpm, overall",
	)
	rpmAccess = m.NewGauge(
		"",
		"",
		"http_open_rpm",
		"rpm, opened static",
	)
	rpmAgree = m.NewGauge(
		"",
		"",
		"http_requests_agreed_rpm",
		"rpm, pressed the button 'agree'",
	)
	rpmErrors = m.NewGauge(
		"",
		"",
		"http_requests_errors_rpm",
		"rpm, errors",
	)

	rpmContentDeliveryErrors = m.NewGauge(
		"",
		"contentd",
		"content_delivery_errors",
		"rpm, content delivery errors",
	)
	rpmPageNotFoundError = m.NewGauge(
		"",
		"",
		"http_requests_error404",
		"rpm, 404 requests",
	)
	rpmContentdRPCDialError = m.NewGauge(
		"",
		"contentd",
		"rpc_contentd_errors",
		"rpm, number of errors connected with RPC contentd",
	)

	LoadCampaignError = m.NewGauge(
		"",
		"",
		"load_campaign_html_error",
		"Load campaign HTML error",
	)
	go func() {
		// metrics in prometheus as for 15s (default)
		// so make for minute interval
		for range time.Tick(time.Minute) {
			rpmOverall.Set(float64(Overall))
			rpmAccess.Set(float64(Acess))
			rpmAgree.Set(float64(Agree))
			rpmErrors.Set(float64(Errors))
			rpmPageNotFoundError.Set(float64(PageNotFoundError))
			rpmContentDeliveryErrors.Set(float64(ContentDeliveryErrors))
			rpmContentdRPCDialError.Set(float64(ContentdRPCDialError))

			Overall = 0
			Acess = 0
			Agree = 0
			Errors = 0
			PageNotFoundError = 0
			ContentDeliveryErrors = 0
			ContentdRPCDialError = 0
		}
	}()
}
