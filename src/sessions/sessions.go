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
	store = sessions.NewCookieStore([]byte(conf.Secret))
	options := &sessions.Options{
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

func SetSession(c *gin.Context) {
	var tid string
	session := sessions.Default(c)
	v := session.Get("tid")
	if v == nil {
		log.WithField("headers", c.Request.Header).Debug("no session found")
		u4, err := uuid.NewV4()
		if err != nil {
			log.WithField("error", err.Error()).Error("generate uniq id")
		}
		tid = fmt.Sprintf("%d-%s", time.Now().Unix(), u4)
		log.WithField("tid", tid).Debug("generated tid")
	} else {
		log.WithField("tid", v).Debug("already have tid")
	}
	session.Set("tid", tid)
	session.Save()
	log.WithField("tid", tid).Info("session seved")
}

func GetTid(c *gin.Context) string {
	session := sessions.Default(c)
	v := session.Get("tid")
	if v == nil {
		log.WithField("headers", c.Request.Header).Error("no tid")
		return ""
	} else {
		log.WithField("tid", v).Debug("found tid")
		return fmt.Sprintf("%s", v)
	}
}
