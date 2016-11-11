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
	MaxAge   int    `default:"87400" yaml:"cookie_ttl"` // MaxAge>0 means Max-Age attribute present and given in seconds.
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

// tid example 1477597462-3f66f7ea-afef-42a2-69ad-549a6a38b5ff
func SetSession(c *gin.Context) {
	log.WithFields(log.Fields{"path": c.Request.URL.String()}).Debug("set session")

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
	session.Set("tid", tid)

	msisdn := getFromParamsOrSession(c, "msisdn", session, "msisdn", 5)
	session.Set("msisdn", msisdn)

	pixel := getFromParamsOrSession(c, "aff_sub", session, "pixel", 5)
	session.Set("pixel", pixel)

	publisher := getFromParamsOrSession(c, "aff_pr", session, "publisher", 5)
	session.Set("publisher", publisher)

	session.Save()
	log.WithFields(log.Fields{"tid": tid, "path": c.Request.URL.Path}).Info("session saved")
}

func getFromParamsOrSession(
	c *gin.Context,
	getParamName string,
	session sessions.Session,
	sessParamName string,
	length int,
) string {
	val, ok := c.GetQuery(getParamName)
	if ok {
		log.WithField(sessParamName, val).Debug("found " + sessParamName + " in get params")
		return val
	}

	v := session.Get(sessParamName)
	if v == nil || len(string(v.(string))) < length {
		log.WithField("sesskey", sessParamName).Debug("not found")
		return ""
	}
	log.WithField(sessParamName, v).Debug("found in session")
	return string(v.(string))

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
func RemoveTid(c *gin.Context) {
	session := sessions.Default(c)
	session.Set("tid", "")
	session.Save()
}
func GetFromSession(what string, c *gin.Context) string {
	session := sessions.Default(c)
	v := session.Get(what)
	if v == nil || len(string(v.(string))) < 5 {
		log.WithField("headers", c.Request.Header).Debug("no " + what)
		return ""
	} else {
		log.WithField(what, v).Debug("found " + what + " in session")
		return string(v.(string))
	}
}
