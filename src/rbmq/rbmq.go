package rbmq

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	inmem_service "github.com/vostrok/inmem/service"
	"github.com/vostrok/utils/amqp"
	"github.com/vostrok/utils/rec"
)

type Notifier interface {
	NewSubscriptionNotify(string, rec.Record) error

	AccessCampaignNotify(msg AccessCampaignNotify) error

	ActionNotify(msg UserActionsNotify) error

	ContentSentNotify(msg inmem_service.ContentSentProperties) error
}

type NotifierConfig struct {
	Queues       Queues              `yaml:"queues"`
	RBMQNotifier amqp.NotifierConfig `yaml:"rbmq"`
}
type Queues struct {
	AccessCampaign string `yaml:"access_campaign" default:"access_campaign"`
	UserAction     string `yaml:"user_actions" default:"user_actions"`
	ContentSent    string `yaml:"content_sent" default:"content_sent"`
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

func (service notifier) NewSubscriptionNotify(queue string, msg rec.Record) error {
	msg.SentAt = time.Now().UTC()
	event := EventNotify{
		EventName: "new_subscription",
		EventData: msg,
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	log.Debugf("new subscription %s", body)
	service.mq.Publish(amqp.AMQPMessage{queue, 0, body})
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
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(amqp.AMQPMessage{service.q.AccessCampaign, 0, body})
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
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	service.mq.Publish(amqp.AMQPMessage{service.q.UserAction, 0, body})
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
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(amqp.AMQPMessage{service.q.ContentSent, 0, body})
	return nil
}
