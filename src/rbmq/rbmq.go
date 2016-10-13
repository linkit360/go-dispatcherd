package rbmq

import (
	"encoding/json"

	"github.com/Sirupsen/logrus"
	"github.com/vostrok/contentd/service"
	"github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/rabbit"
)

type Notifier interface {
	NewSubscriptionNotify(service.MsgRecordContentSent) error
}
type notifier struct {
	queue string
	mq    rabbit.AMQPService
}

type EventNotify struct {
	EventName string      `json:"event_name,omitempty"`
	EventData interface{} `json:"event_data,omitempty"`
}

func NewNotifierService(queueName string, conf rabbit.RBMQConfig) Notifier {
	var n Notifier
	{
		rabbit := rabbit.New(rabbit.RBMQConfig{
			Url:            conf.Url,
			PublishChanCap: conf.PublishChanCap,
			Metrics:        metrics.M.RBMQMetrics,
		})

		n = &notifier{
			queue: queueName,
			mq:    rabbit,
		}
	}
	return n
}

func (service notifier) NewSubscriptionNotify(msg service.MsgRecordContentSent) error {

	event := EventNotify{
		EventName: service.queue,
		EventData: msg,
	}

	body, err := json.Marshal(event)
	if err != nil {
		logrus.WithField("NewSubscriptionNotify", err.Error())
		return err
	}

	service.mq.Publish(rabbit.AMQPMessage{service.queue, body})
	return nil
}
