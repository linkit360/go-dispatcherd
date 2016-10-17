package rbmq

import (
	"encoding/json"

	"github.com/Sirupsen/logrus"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/rabbit"
)

type Notifier interface {
	NewSubscriptionNotify(service.ContentSentProperties) error

	AccessCampaignNotify(msg AccessCampaignNotify) error
}

type NotifierConfig struct {
	Queues struct {
		NewSubscriptionQueueName string `yaml:"new_subscription"`
		AccessCampaignQueueName  string `yaml:"access_campaign"`
	} `yaml:"queues"`
	Rbmq rabbit.RBMQConfig `yaml:"rabbit"`
}
type queues struct {
	newSubscription string
	accessCampaign  string
}
type notifier struct {
	q  queues
	mq rabbit.AMQPService
}

type EventNotify struct {
	EventName string      `json:"event_name,omitempty"`
	EventData interface{} `json:"event_data,omitempty"`
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
		logrus.WithField("NewSubscriptionNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.newSubscription, body})
	return nil
}

type AccessCampaignNotify struct {
	Msisdn              string `json:"msisdn"`
	Tid                 string `json:"tid"`
	IP                  string `json:"ip"`
	OperatorCode        int64  `json:"operator_code"`
	CountryCode         int64  `json:"country_code"`
	Supported           bool   `json:"supported"`
	UserAgent           string `json:"user_agent"`
	Referer             string `json:"referer"`
	UrlPath             string `json:"url_path"`
	Method              string `json:"method"`
	Headers             string `json:"headers"`
	ContentServiceError bool   `json:"content_service_error"`
	ContentFileError    bool   `json:"content_file_error"`
	CampaignId          int64  `json:"campaign_id"`
	ContentId           int64  `json:"content_id"`
	ServiceId           int64  `json:"service_id"`
}

func (service notifier) AccessCampaignNotify(msg AccessCampaignNotify) error {

	event := EventNotify{
		EventName: "access_campaign",
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		logrus.WithField("AccessCampaignNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.q.accessCampaign, body})
	return nil
}
