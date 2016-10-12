package config

import (
	"flag"
	"os"
	//"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/jinzhu/configor"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/rabbit"
)

type AppConfig struct {
	Server        ServerConfig            `yaml:"server"`
	NewRelic      NewRelicConfig          `yaml:"newrelic"`
	RBMQ          rabbit.RBMQConfig       `yaml:"rabbit"`
	Subscriptions SubscriptionsConfig     `yaml:"subscriptions"`
	ContentClient content.RPCClientConfig `yaml:"content_client"`
}
type ServerConfig struct {
	Port          string `default:"70301"`
	RBMQQueueName string `default:"new_subscription" yaml:"queue"`
}
type NewRelicConfig struct {
	AppName string `default:"dispatcherd.linkit360.com"`
	License string `default:"4d635427ad90ca786ca2db6aa246ed651730b933"`
}
type SubscriptionsConfig struct {
	ErrorRedirectUrl   string `default:"http://id.slypee.com" yaml:"error_redirect_url"`
	StaticPath         string `default:"/var/www/xmp.linkit360.ru/web/" yaml:"static_path"`
	CampaignHashLength int    `default:"32" yaml:"campaign_hash_length"`
}

func LoadConfig() AppConfig {
	cfg := flag.String("config", "dev/appconfig.yml", "configuration yml file")
	flag.Parse()
	var appConfig AppConfig

	if *cfg != "" {
		if err := configor.Load(&appConfig, *cfg); err != nil {
			log.WithField("config", err.Error()).Fatal("config load error")
			os.Exit(1)
		}
	}

	appConfig.Server.Port = envString("PORT", appConfig.Server.Port)
	appConfig.Server.RBMQQueueName = envString("QUEUE", appConfig.Server.RBMQQueueName)

	appConfig.ContentClient.DSN = envString("CONTENT_DSN", appConfig.ContentClient.DSN)
	appConfig.ContentClient.Timeout = envString("CONTENT_DSN", appConfig.ContentClient.Timeout)

	appConfig.RBMQ.Url = envString("RBMQ_URL", appConfig.RBMQ.Url)

	log.WithField("config", appConfig).Info("Config loaded")
	return appConfig
}

func envString(env, fallback string) string {
	e := os.Getenv(env)
	if e == "" {
		return fallback
	}
	return e
}
