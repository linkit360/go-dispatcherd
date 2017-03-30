package rbmq

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	inmem_service "github.com/linkit360/go-inmem/service"
	redirect_service "github.com/linkit360/go-partners/service"
	"github.com/linkit360/go-utils/amqp"
	"github.com/linkit360/go-utils/rec"
)

type Notifier interface {
	RedirectNotify(msg redirect_service.DestinationHit) error

	NewSubscriptionNotify(string, rec.Record) error

	AccessCampaignNotify(msg AccessCampaignNotify) error

	ActionNotify(msg UserActionsNotify) error

	ContentSentNotify(msg inmem_service.ContentSentProperties) error

	PixelBufferNotify(r rec.Record) error
}

type NotifierConfig struct {
	Queues       Queues              `yaml:"queues"`
	RBMQNotifier amqp.NotifierConfig `yaml:"rbmq"`
}
type Queues struct {
	AccessCampaign   string `yaml:"access_campaign" default:"access_campaign"`
	UserAction       string `yaml:"user_actions" default:"user_actions"`
	ContentSent      string `yaml:"content_sent" default:"content_sent"`
	PixelSent        string `yaml:"pixel_sent" default:"pixel_sent"`
	TrafficRedirects string `yaml:"traffic_redirects" default:"traffic_redirects"`
}
type notifier struct {
	q  Queues
	mq *amqp.Notifier
}

type EventNotify struct {
	EventName string      `json:"event_name,omitempty"`
	EventData interface{} `json:"event_data,omitempty"`
}

func init() {
	log.SetLevel(log.DebugLevel)
}

func NewNotifierService(conf NotifierConfig) Notifier {
	var n Notifier
	{
		rabbit := amqp.NewNotifier(conf.RBMQNotifier)
		n = &notifier{
			q:  conf.Queues,
			mq: rabbit,
		}
	}
	return n
}

func (service notifier) RedirectNotify(msg redirect_service.DestinationHit) error {
	event := EventNotify{
		EventName: service.q.TrafficRedirects,
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	service.mq.Publish(amqp.AMQPMessage{service.q.TrafficRedirects, uint8(1), body, event.EventName})
	return nil
}

func (service notifier) NewSubscriptionNotify(queue string, msg rec.Record) error {
	msg.SentAt = time.Now().UTC()
	event := EventNotify{
		EventName: "new_subscription",
		EventData: msg,
	}
	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	log.Debugf("new subscription %s", body)
	service.mq.Publish(amqp.AMQPMessage{queue, 0, body, event.EventName})
	return nil
}

type AccessCampaignNotify struct {
	Msisdn       string    `json:"msisdn,omitempty"`
	CampaignHash string    `json:"campaign_hash,omitempty"`
	Tid          string    `json:"tid,omitempty"`
	IP           string    `json:"ip,omitempty"`
	OperatorCode int64     `json:"operator_code,omitempty"`
	CountryCode  int64     `json:"country_code,omitempty"`
	Supported    bool      `json:"supported,omitempty"`
	UserAgent    string    `json:"user_agent,omitempty"`
	Referer      string    `json:"referer,omitempty"`
	UrlPath      string    `json:"url_path,omitempty"`
	Method       string    `json:"method,omitempty"`
	Headers      string    `json:"headers,omitempty"`
	Error        string    `json:"err,omitempty"`
	CampaignId   int64     `json:"campaign_id,omitempty"`
	ContentId    int64     `json:"content_id,omitempty"`
	ServiceId    int64     `json:"service_id,omitempty"`
	SentAt       time.Time `json:"sent_at,omitempty"`
}

func (service notifier) AccessCampaignNotify(msg AccessCampaignNotify) error {
	msg.SentAt = time.Now().UTC()
	event := EventNotify{
		EventName: "access_campaign",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(amqp.AMQPMessage{service.q.AccessCampaign, 0, body, event.EventName})
	return nil
}

type UserActionsNotify struct {
	Tid        string    `json:"tid,omitempty"`
	CampaignId int64     `json:"campaign_id,omitempty"`
	Msisdn     string    `json:"msisdn,omitempty"`
	Error      string    `json:"err,omitempty"`
	Action     string    `json:"action,omitempty"`
	SentAt     time.Time `json:"sent_at,omitempty"`
}

func (service notifier) ActionNotify(msg UserActionsNotify) error {
	if msg.Msisdn == "" {
		return nil
	}
	msg.SentAt = time.Now().UTC()
	event := EventNotify{
		EventName: "user_actions",
		EventData: msg,
	}
	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	service.mq.Publish(amqp.AMQPMessage{service.q.UserAction, 0, body, event.EventName})
	return nil
}

func (service notifier) ContentSentNotify(msg inmem_service.ContentSentProperties) error {
	msg.SentAt = time.Now().UTC()

	event := EventNotify{
		EventName: "content_sent",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(amqp.AMQPMessage{service.q.ContentSent, 0, body, event.EventName})
	return nil
}

func (service notifier) PixelBufferNotify(r rec.Record) error {
	event := EventNotify{
		EventName: "buffer",
		EventData: r,
	}

	body, err := json.Marshal(event)
	if err != nil {
		m.NotifyError.Inc()
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	service.mq.Publish(amqp.AMQPMessage{service.q.PixelSent, uint8(1), body, event.EventName})
	return nil
}
