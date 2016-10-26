package rbmq

import (
	"encoding/json"

	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/rabbit"
)

type Notifier interface {
	NewSubscriptionNotify(service.ContentSentProperties) error

	AccessCampaignNotify(msg AccessCampaignNotify) error

	ActionNotify(msg UserActionsNotify) error
}

type NotifierConfig struct {
	Queues struct {
		NewSubscriptionQueueName string `yaml:"new_subscription" default:"new_subscription"`
		AccessCampaignQueueName  string `yaml:"access_campaign" default:"access_campaign"`
		UserActionsQueueName     string `yaml:"user_actions" default:"user_actions"`
	} `yaml:"queues"`
	Rbmq rabbit.RBMQConfig `yaml:"rabbit"`
}
type queues struct {
	newSubscription      string
	accessCampaign       string
	userAction           string
	accessCampaignUpdate string
}
type notifier struct {
	q  queues
	mq rabbit.AMQPService
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
		rabbit := rabbit.NewPublisher(rabbit.RBMQConfig{
			Url:     conf.Rbmq.Url,
			ChanCap: conf.Rbmq.ChanCap,
			Metrics: rabbit.InitMetrics(),
		})

		n = &notifier{
			q: queues{
				newSubscription: conf.Queues.NewSubscriptionQueueName,
				accessCampaign:  conf.Queues.AccessCampaignQueueName,
				userAction:      conf.Queues.UserActionsQueueName,
			},
			mq: rabbit,
		}
	}
	return n
}

func (service notifier) NewSubscriptionNotify(msg service.ContentSentProperties) error {

	event := EventNotify{
		EventName: "new_subscription",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.newSubscription, body})
	return nil
}

type AccessCampaignNotify struct {
	Msisdn       string `json:"msisdn,omitempty"`
	CampaignHash string `json:"campaoign_hash,omitempty"`
	Tid          string `json:"tid,omitempty"`
	IP           string `json:"ip,omitempty"`
	OperatorCode int64  `json:"operator_code,omitempty"`
	CountryCode  int64  `json:"country_code,omitempty"`
	Supported    bool   `json:"supported,omitempty"`
	UserAgent    string `json:"user_agent,omitempty"`
	Referer      string `json:"referer,omitempty"`
	UrlPath      string `json:"url_path,omitempty"`
	Method       string `json:"method,omitempty"`
	Headers      string `json:"headers,omitempty"`
	Error        string `json:"error,omitempty"`
	CampaignId   int64  `json:"campaign_id,omitempty"`
	ContentId    int64  `json:"content_id,omitempty"`
	ServiceId    int64  `json:"service_id,omitempty"`
}

func (service notifier) AccessCampaignNotify(msg AccessCampaignNotify) error {

	event := EventNotify{
		EventName: "access_campaign",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.accessCampaign, body})
	return nil
}

type UserActionsNotify struct {
	Tid    string `json:"tid,omitempty"`
	Error  string `json:"error,omitempty"`
	Action string `json:"action,omitempty"`
}

func (service notifier) ActionNotify(msg UserActionsNotify) error {
	log.WithField("msg", msg).Debug("got msg")

	event := EventNotify{
		EventName: "user_actions",
		EventData: msg,
	}
	log.WithField("event", event).Debug("got event")
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("json.Marshal: %s", err.Error())
	}
	log.WithField("body", string(body)).Debug("sent body")
	service.mq.Publish(rabbit.AMQPMessage{service.q.userAction, body})
	return nil
}
