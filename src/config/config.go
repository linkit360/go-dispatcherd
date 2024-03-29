package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jinzhu/configor"
	log "github.com/sirupsen/logrus"

	content_client "github.com/linkit360/go-contentd/rpcclient"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	mid "github.com/linkit360/go-mid/rpcclient"
	redirect_client "github.com/linkit360/go-partners/rpcclient"
)

type AppConfig struct {
	AppName        string                          `yaml:"app_name"`
	Server         ServerConfig                    `yaml:"server"`
	Service        ServiceConfig                   `yaml:"service"`
	ContentClient  content_client.ClientConfig     `yaml:"content_client"`
	MidConfig      mid.ClientConfig                `yaml:"mid_client"`
	RedirectConfig redirect_client.RPCClientConfig `yaml:"redirect_client"`
	Notifier       rbmq.NotifierConfig             `yaml:"notifier"`
}

type ServerConfig struct {
	Host     string                  `default:"127.0.0.1"`
	Port     string                  `default:"50300"`
	Path     string                  `default:"/var/www/xmp.linkit360.ru/web/" yaml:"path"`
	Url      string                  `default:"http://platform.pk.linkit360.ru" yaml:"url"`
	Sessions sessions.SessionsConfig `yaml:"sessions"`
}
type ServiceConfig struct {
	ContentServiceCodeDefault string         `yaml:"content_service_code_default"`
	ContentCampaignIdDefault  string         `yaml:"content_campaign_id_default"`
	ErrorRedirectUrl          string         `default:"http://id.slypee.com" yaml:"error_redirect_url"`
	NotFoundRedirectUrl       string         `default:"http://id.slypee.com" yaml:"not_found_redirect_url"`
	RedirectOnGatherError     bool           `yaml:"redirect_on_gather_error"`
	SendRestorePixelEnabled   bool           `yaml:"send_restore_pixel_enabled"`
	DetectByIpEnabled         bool           `yaml:"detect_by_ip_enabled"`
	OnClickNewSubscription    bool           `yaml:"start_new_subscription_on_click"`
	CampaignHashLength        int            `yaml:"campaign_hash_length" default:"32"`
	Rejected                  RejectedConfig `yaml:"rejected"`
	OperatorCode              int64          `yaml:"operator_code" default:"25099"`
	CountryCode               int64          `yaml:"country_code" default:"7"`
	LandingPages              LPsConfig      `yaml:"landings"`
}

type RejectedConfig struct {
	CampaignRedirectEnabled bool `default:"false" yaml:"campaign_redirect_enabled"`
	TrafficRedirectEnabled  bool `default:"false" yaml:"traffic_redirect_enabled"`
}

type LPsConfig struct {
	Custom   bool                `yaml:"custom"`
	Beeline  BeelineLandingConf  `yaml:"beeline"`
	QRTech   QRTechLandingConf   `yaml:"qrtech"`
	Mobilink MobilinkLandingConf `yaml:"mobilink"`
}

type BeelineLandingConf struct {
	Enabled      bool   `yaml:"enabled"`
	Url          string `yaml:"url"`
	SessionPath  string `yaml:"session_path"`
	MOQueue      string `yaml:"mo"`
	OperatorCode int64  `yaml:"operator_code" default:"25099"`
	CountryCode  int64  `yaml:"country_code" default:"7"`
	Timeout      int    `yaml:"timeout"`
	Auth         struct {
		User string `yaml:"user"`
		Pass string `yaml:"pass"`
	} `yaml:"auth"`
}

type QRTechLandingConf struct {
	Enabled          bool   `yaml:"enabled"`
	CountryCode      int64  `yaml:"country_code" default:"66"`
	DtacOperatorCode int64  `yaml:"dtac_operator_code" default:"52005"`
	AisOperatorCode  int64  `yaml:"ais_operator_code" default:"52001"`
	AisUrl           string `yaml:"ais_url"`
	DtacUrl          string `yaml:"dtac_url"`
	AutoclickUrl     string `yaml:"autoclick_url"`
	Timeout          int    `yaml:"timeout"`
	ContentUrl       string `yaml:"content_url"`
	AesKey           string `yaml:"aes_key"`
	Auth             struct {
		User string `yaml:"user"`
		Pass string `yaml:"pass"`
	} `yaml:"auth"`
}

type MobilinkLandingConf struct {
	Enabled      bool  `yaml:"enabled"`
	OperatorCode int64 `yaml:"operator_code" default:"41001"`
	CountryCode  int64 `yaml:"country_code" default:"92"`

	Queues struct {
		MO        string `yaml:"mo"`
		Responses string `yaml:"responses"`
	} `yaml:"queues"`
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

	if appConfig.Service.Rejected.TrafficRedirectEnabled &&
		!appConfig.RedirectConfig.Enabled {
		log.Infof("implicitly enabled redirect service")
		appConfig.RedirectConfig.Enabled = true
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
