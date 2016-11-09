package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var M Metrics

type Metrics struct {
	RequestsOverall       prometheus.Counter
	RequestsAccess        prometheus.Counter
	RequestsAgree         prometheus.Counter
	RequestsErrors        prometheus.Counter
	CampaignPageError     prometheus.Gauge
	ContentDeliveryErrors prometheus.Counter
	PageNotFoundError     prometheus.Counter
	ContentdRPCDialError  prometheus.Counter
}

func Init() {
	M = Metrics{
		RequestsOverall:       newCounter("requests_overall", "Number of requests overall"),
		RequestsAccess:        newCounter("requests_access", "Number of requests access the campaign static page"),
		RequestsAgree:         newCounter("requests_agree", "Number of requests pressed the button"),
		RequestsErrors:        newCounter("errors_requests", "Number of requests pressed the button"),
		CampaignPageError:     newGauge("campaign_page_not_found", "campaigns", "If 0 - all campaigns loaded ok. If 1 - there were errors during the load of campaign html"),
		ContentDeliveryErrors: newCounter("content_delivery_errors", "Number of requests with content delivery error"),
		PageNotFoundError:     newCounter("errors_404", "Number of 404 requests"),
		ContentdRPCDialError:  newCounter("errors_contentd_rpc", "Number of errors connected with RPC contentd"),
	}
}

func newCounter(name, help string) prometheus.Counter {
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: help,
	})
	prometheus.MustRegister(counter)
	return counter
}

func newGauge(name, subsystem, help string) prometheus.Gauge {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "dispatcherd",
		Subsystem: subsystem,
		Name:      name,
		Help:      help,
	})
	prometheus.MustRegister(gauge)
	return gauge
}
