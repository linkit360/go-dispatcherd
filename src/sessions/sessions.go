package sessions

import (
	"time"

	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/nu7hatch/gouuid"
)

var store sessions.CookieStore

type SessionsConfig struct {
	Secret   string `default:"rCs7h2h_NqB5Kx-" yaml:"secret"`
	Path     string `default:"/" yaml:"path"`
	Domain   string `default:"pk.linkit360.ru" yaml:"domain"`
	MaxAge   int    `default:"3600" yaml:"cookie_ttl"` // MaxAge>0 means Max-Age attribute present and given in seconds.
	Secure   bool   `default:"false" yaml:"secure"`
	HttpOnly bool   `default:"false" yaml:"http_only"`
}

func Init(conf SessionsConfig, r *gin.Engine) {
	log.SetLevel(log.DebugLevel)

	store = sessions.NewCookieStore([]byte(conf.Secret))
	options := sessions.Options{
		Path:     conf.Path,
		Domain:   conf.Domain,
		MaxAge:   conf.MaxAge,
		Secure:   conf.Secure,
		HttpOnly: conf.HttpOnly,
	}
	store.Options(options)

	r.Use(sessions.Sessions("sess", store))
}

func AddSessionTidHandler(c *gin.Context) {
	SetSession(c)
	c.Next()
}

// tid example 1477597462-3f66f7ea-afef-42a2-69ad-549a6a38b5ff
func SetSession(c *gin.Context) {
	log.Debug("set session")

	var tid string
	session := sessions.Default(c)
	v := session.Get("tid")

	if v == nil || len(string(v.(string))) < 40 {
		log.WithField("headers", c.Request.Header).Debug("no session found")
		u4, err := uuid.NewV4()
		if err != nil {
			log.WithField("error", err.Error()).Error("generate uniq id")
		}
		tid = fmt.Sprintf("%d-%s", time.Now().Unix(), u4)
		log.WithField("tid", tid).Debug("generated tid")
	} else {
		log.WithField("tid", v).Debug("already have tid")
		tid = string(v.(string))
	}
	if msisdn, ok := c.GetQuery("msisdn"); ok {
		log.WithField("msisdn", msisdn).Debug("found msisdn")
		session.Set("msisdn", msisdn)
	}
	session.Set("tid", tid)
	session.Save()
	log.WithField("tid", tid).Info("session saved")
}

func GetTid(c *gin.Context) string {
	session := sessions.Default(c)
	v := session.Get("tid")
	if v == nil || len(string(v.(string))) < 40 {
		log.WithField("headers", c.Request.Header).Error("no tid")
		return ""
	} else {
		log.WithField("tid", v).Debug("found tid")
		return fmt.Sprintf("%s", v)
	}
}
func GetMsisdn(c *gin.Context) string {
	session := sessions.Default(c)
	v := session.Get("msisdn")
	if v == nil || len(string(v.(string))) < 5 {
		log.WithField("headers", c.Request.Header).Debug("no msisdn")
		return ""
	} else {
		log.WithField("msisdn", v).Debug("found msisdn")
		return string(v.(string))
	}
}
