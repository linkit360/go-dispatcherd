package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/jinzhu/configor"

	content "github.com/vostrok/contentd/rpcclient"
	"github.com/vostrok/dispatcherd/src/rbmq"
	"github.com/vostrok/dispatcherd/src/sessions"
	inmem "github.com/vostrok/inmem/rpcclient"
)

type AppConfig struct {
	MetricInstancePrefix string                  `yaml:"metric_instance_prefix"`
	AppName              string                  `yaml:"app_name"`
	Server               ServerConfig            `yaml:"server"`
	Service              ServiceConfig           `yaml:"service"`
	ContentClient        content.RPCClientConfig `yaml:"content_client"`
	InMemConfig          inmem.RPCClientConfig   `yaml:"inmem_client"`
	Notifier             rbmq.NotifierConfig     `yaml:"notifier"`
}

type ServerConfig struct {
	Port     string                  `default:"50300"`
	Path     string                  `default:"/var/www/xmp.linkit360.ru/web/" yaml:"path"`
	Url      string                  `default:"http://dev.pk.linkit360.ru/" yaml:"url"`
	Sessions sessions.SessionsConfig `yaml:"sessions"`
}
type ServiceConfig struct {
	ErrorRedirectUrl   string         `default:"http://id.slypee.com" yaml:"error_redirect_url"`
	CampaignHashLength int            `default:"32" yaml:"campaign_hash_length"`
	Rejected           RejectedConfig `yaml:"rejected"`
}

type RejectedConfig struct {
	Enabled bool `default:"true" yaml:"enabled"`
}

func LoadConfig() AppConfig {
	cfg := flag.String("config", "dev/dispatcherd.yml", "configuration yml file")
	flag.Parse()
	var appConfig AppConfig

	if *cfg != "" {
		if err := configor.Load(&appConfig, *cfg); err != nil {
			log.WithField("config", err.Error()).Fatal("config load error")
			os.Exit(1)
		}
	}

	if appConfig.AppName == "" {
		log.Fatal("app name must be defiled as <host>_<name>")
	}
	if strings.Contains(appConfig.AppName, "-") {
		log.Fatal("app name must be without '-' : it's not a valid metric name")
	}

	if appConfig.MetricInstancePrefix == "" {
		log.Fatal("metric_instance_prefix be defiled as <host>_<name>")
	}
	if strings.Contains(appConfig.MetricInstancePrefix, "-") {
		log.Fatal("metric_instance_prefix be without '-' : it's not a valid metric name")
	}

	appConfig.Server.Port = envString("PORT", appConfig.Server.Port)
	appConfig.Server.Path = envString("SERVER_PATH", appConfig.Server.Path)

	appConfig.ContentClient.DSN = envString("CONTENT_DSN", appConfig.ContentClient.DSN)
	appConfig.ContentClient.Timeout = envInt("CONTENT_TIMEOUT", appConfig.ContentClient.Timeout)

	appConfig.Notifier.RBMQNotifier.Conn.Host = envString("RBMQ_HOST", appConfig.Notifier.RBMQNotifier.Conn.Host)

	log.WithField("config", fmt.Sprintf("%#v", appConfig)).Info("Config")
	return appConfig
}

func envString(env, fallback string) string {
	e := os.Getenv(env)
	if e == "" {
		return fallback
	}
	return e
}

func envInt(env string, fallback int) int {
	e := os.Getenv(env)
	d, err := strconv.Atoi(e)
	if err != nil {
		return fallback
	}
	return d
}
