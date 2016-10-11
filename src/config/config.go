package config

import (
	"flag"
	"os"
	//"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/jinzhu/configor"

	"github.com/vostrok/rabbit"
)

type AppConfig struct {
	Server        ServerConfig        `yaml:"server"`
	NewRelic      NewRelicConfig      `yaml:"newrelic"`
	RBMQ          rabbit.RBMQConfig   `yaml:"rabbit"`
	Subscriptions SubscriptionsConfig `yaml:"subscriptions"`
}
type ServerConfig struct {
	Port          string `default:"70301"`
	RBMQQueueName string `default:"new_subscription" yaml:"queue"`
}
type NewRelicConfig struct {
	AppName string `default:"dispatcherd"`
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

	appConfig.Server.Port = EnvString("PORT", appConfig.Server.Port)

	log.WithField("config", appConfig).Info("Config loaded")
	return appConfig
}

func EnvString(env, fallback string) string {
	e := os.Getenv(env)
	if e == "" {
		return fallback
	}
	return e
}

//
//func EnvInt(env string, fallback int) int {
//	e := os.Getenv(env)
//	d, err := strconv.Atoi(e)
//	if err != nil {
//		return fallback
//	}
//	return d
//}
//
//func EnvInt64(env string, fallback int64) int64 {
//	e := os.Getenv(env)
//	d, err := strconv.ParseInt(e, 10, 64)
//	if err != nil {
//		return fallback
//	}
//	return d
//}
//
//func EnvBool(env string, fallback bool) bool {
//	e := os.Getenv(env)
//
//	if e == "true" {
//		return true
//	}
//	if e == "false" {
//		return false
//	}
//	return fallback
//}
