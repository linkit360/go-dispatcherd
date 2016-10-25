package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/dispatcherd/src/campaigns"
	"github.com/vostrok/dispatcherd/src/operator"
)

type response struct {
	Success bool        `json:"success,omitempty"`
	Err     error       `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Status  int         `json:"-"`
}

func AddCQRHandler(r *gin.Engine) {
	rg := r.Group("/cqr")
	rg.GET("", Reload)
}

func Reload(c *gin.Context) {
	var err error
	r := response{Err: err, Status: http.StatusOK}

	table, exists := c.GetQuery("table")
	if !exists || table == "" {
		table, exists = c.GetQuery("t")
		if !exists || table == "" {
			err := errors.New("Table name required")
			r.Status = http.StatusBadRequest
			r.Err = err
			render(r, c)
			return
		}
	}

	switch {
	case strings.Contains(table, "operator_ip"):
		if err := operator.Reload(); err != nil {
			r.Success = false
			r.Status = http.StatusInternalServerError
			log.WithField("error", err.Error()).Error("Load IP ranges fail")
		} else {
			r.Success = true
		}
	case strings.Contains(table, "campaigns"):
		if err := campaigns.Reload(); err != nil {
			r.Success = false
			r.Status = http.StatusInternalServerError
			log.WithField("error", err.Error()).Error("Load IP ranges fail")
		} else {
			r.Success = true
		}
	default:
		err = fmt.Errorf("Table name %s not recognized", table)
		r.Status = http.StatusBadRequest
	}
	render(r, c)
	return
}

func render(msg response, c *gin.Context) {
	if msg.Err != nil {
		c.Header("Error", msg.Err.Error())
		c.Error(msg.Err)
	}
	c.JSON(msg.Status, msg)
}
