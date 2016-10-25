package rbmq

import (
	"encoding/json"

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
		log.WithField("NewSubscriptionNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.newSubscription, body})
	return nil
}

type AccessCampaignNotify struct {
	Msisdn       string `json:"msisdn"`
	CampaignHash string `json:"campaoign_hash"`
	Tid          string `json:"tid"`
	IP           string `json:"ip"`
	OperatorCode int64  `json:"operator_code"`
	CountryCode  int64  `json:"country_code"`
	Supported    bool   `json:"supported"`
	UserAgent    string `json:"user_agent"`
	Referer      string `json:"referer"`
	UrlPath      string `json:"url_path"`
	Method       string `json:"method"`
	Headers      string `json:"headers"`
	Error        string `json:"error"`
	CampaignId   int64  `json:"campaign_id"`
	ContentId    int64  `json:"content_id"`
	ServiceId    int64  `json:"service_id"`
}

func (service notifier) AccessCampaignNotify(msg AccessCampaignNotify) error {

	event := EventNotify{
		EventName: "access_campaign",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.WithField("AccessCampaignNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.accessCampaign, body})
	return nil
}

type UserActionsNotify struct {
	Tid    string `json:"tid"`
	Error  string `json:"error"`
	Action string `json:"tid"`
}

func (service notifier) ActionNotify(msg UserActionsNotify) error {
	event := EventNotify{
		EventName: "user_actions",
		EventData: msg,
	}
	body, err := json.Marshal(event)
	if err != nil {
		log.WithField("ActionNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.userAction, body})
	return nil
}
