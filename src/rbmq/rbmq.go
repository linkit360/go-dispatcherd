package rbmq

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/utils/amqp"
)

type Notifier interface {
	NewSubscriptionNotify(string, *service.ContentSentProperties) error

	AccessCampaignNotify(msg AccessCampaignNotify) error

	ActionNotify(msg UserActionsNotify) error
}

type NotifierConfig struct {
	Queues struct {
		AccessCampaignQueueName string `yaml:"access_campaign" default:"access_campaign"`
		UserActionsQueueName    string `yaml:"user_actions" default:"user_actions"`
	} `yaml:"queues"`
	RBMQNotifier amqp.NotifierConfig `yaml:"rbmq"`
}
type queues struct {
	accessCampaign       string
	userAction           string
	accessCampaignUpdate string
}
type notifier struct {
	q  queues
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
			q: queues{
				accessCampaign: conf.Queues.AccessCampaignQueueName,
				userAction:     conf.Queues.UserActionsQueueName,
			},
			mq: rabbit,
		}
	}
	return n
}

func (service notifier) NewSubscriptionNotify(queue string, msg *service.ContentSentProperties) error {
	log.WithFields(log.Fields{
		"event":  "new_subscription",
		"tid":    msg.Tid,
		"msisdn": msg.Msisdn,
	}).Debug("got event")

	event := EventNotify{
		EventName: "new_subscription",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	log.WithField("body", string(body)).Debug("sent")
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

	service.mq.Publish(amqp.AMQPMessage{service.q.accessCampaign, 0, body})
	return nil
}

type UserActionsNotify struct {
	Tid    string    `json:"tid,omitempty"`
	Error  string    `json:"err,omitempty"`
	Action string    `json:"action,omitempty"`
	SentAt time.Time `json:"sent_at,omitempty"`
}

func (service notifier) ActionNotify(msg UserActionsNotify) error {
	msg.SentAt = time.Now().UTC()

	event := EventNotify{
		EventName: "user_actions",
		EventData: msg,
	}
	log.WithField("event", event).Debug("got event")
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	log.WithField("body", string(body)).Debug("sent")
	service.mq.Publish(amqp.AMQPMessage{service.q.userAction, 0, body})
	return nil
}
