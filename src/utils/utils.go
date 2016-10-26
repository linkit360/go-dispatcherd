package utils

import (
	"fmt"
	"io/ioutil"

	"github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"
	"strings"
)

func ServeAttachment(filePath, name string, c *gin.Context, log *logrus.Entry) error {
	log.WithField("path", filePath).Debug("serve file")

	w := c.Writer

	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.WithField("error", err.Error()).Error("ioutil.ReadFile serve file error")
		err := fmt.Errorf("ioutil.ReadFile: %s", err.Error())
		return err
	}

	ff := strings.Split(filePath, ".")
	fileName := name + "." + ff[len(ff)-1]

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("Content-Type", "application; charset-utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(200)
	w.Write(content)
	return nil
}

func ServeStaticFile(filePath string, c *gin.Context, log *logrus.Entry) error {
	log.WithField("path", filePath).Debug("serve file")

	w := c.Writer

	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.WithField("error", err.Error()).Error("ioutil.ReadFile serve file error")
		err := fmt.Errorf("ioutil.ReadFile: %s", err.Error())
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset-utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(200)
	w.Write(content)
	return nil
}
