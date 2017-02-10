package sessions

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"

	rec "github.com/vostrok/utils/rec"
)

var store sessions.CookieStore

type SessionsConfig struct {
	Secret   string `default:"rCs7h2h_NqB5Kx-" yaml:"secret"`
	Path     string `default:"/" yaml:"path"`
	Domain   string `default:"pk.linkit360.ru" yaml:"domain"`
	MaxAge   int    `default:"300" yaml:"cookie_ttl"` // MaxAge>0 means Max-Age attribute present and given in seconds.
	Secure   bool   `default:"false" yaml:"secure"`
	HttpOnly bool   `default:"false" yaml:"http_only"`
	Key      string `default:"sehB33772" yaml:"key"`
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

	r.Use(sessions.Sessions(conf.Key, store))
}

// tid example 1477597462-3f66f7ea-afef-42a2-69ad-549a6a38b5ff
func SetSession(c *gin.Context) {
	log.WithFields(log.Fields{"path": c.Request.URL.String()}).Debug("set session")

	var tid string
	session := sessions.Default(c)
	v := session.Get("tid")

	if v == nil || len(string(v.(string))) < 40 {
		log.WithField("headers", c.Request.Header).Debug("no session found")
		tid = rec.GenerateTID()
	} else {
		log.WithField("tid", v).Debug("already have tid")
		tid = string(v.(string))
	}
	session.Set("tid", tid)

	msisdn := getFromParamsOrSession(tid, c, "msisdn", session, "msisdn", 5)
	session.Set("msisdn", msisdn)

	pixel := getFromParamsOrSession(tid, c, "aff_sub", session, "pixel", 5)
	session.Set("pixel", pixel)

	publisher := getFromParamsOrSession(tid, c, "aff_pr", session, "publisher", 5)
	session.Set("publisher", publisher)

	session.Save()
	log.WithFields(log.Fields{"tid": tid, "path": c.Request.URL.Path}).Info("session saved")
}

func Set(name string, val interface{}, c *gin.Context) {
	session := sessions.Default(c)
	session.Set(name, val)
}

func getFromParamsOrSession(
	tid string,
	c *gin.Context,
	getParamName string,
	session sessions.Session,
	sessParamName string,
	length int,
) string {
	val, ok := c.GetQuery(getParamName)
	if ok && len(val) >= length {
		log.WithFields(log.Fields{
			"tid":         tid,
			sessParamName: val,
		}).Debug("found " + sessParamName + " in get params")
		return val
	}

	v := session.Get(sessParamName)
	if v == nil || len(string(v.(string))) < length {
		log.WithFields(log.Fields{
			"tid":     tid,
			"sesskey": sessParamName,
		}).Debug(sessParamName + " not found")
		return ""
	}
	log.WithFields(log.Fields{
		"tid":         tid,
		sessParamName: v,
	}).Debug("found in session")
	return string(v.(string))

}
func GetTid(c *gin.Context) string {
	session := sessions.Default(c)
	v := session.Get("tid")
	if v == nil || len(string(v.(string))) < 40 {
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
		log.Debug("no " + what)
		return ""
	} else {
		log.WithField(what, v).Debug("found " + what + " in session")
		return string(v.(string))
	}
}
