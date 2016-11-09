package campaigns

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/db"
	m "github.com/vostrok/dispatcherd/src/metrics"
	"github.com/vostrok/dispatcherd/src/utils"
)

var camp *campaign

const ACTIVE_STATUS = 1

type campaign struct {
	dbConn     *sql.DB
	dbConf     db.DataBaseConfig
	staticPath string
	campaigns  *Campaigns
}

func Get() *Campaigns {
	return camp.campaigns
}

func Init(static string, conf db.DataBaseConfig) {
	logrus.SetLevel(logrus.DebugLevel)

	camp = &campaign{
		dbConn:     db.Init(conf),
		dbConf:     conf,
		staticPath: static,
		campaigns:  &Campaigns{},
	}

	err := Reload()
	if err != nil {
		logrus.WithField("error", err.Error()).Fatal("reload campaigns failed")
	}
}

// Tasks:
// Keep in memory all active campaigns
// Allow to get a campaign information by campaign link fastly
// Reload when changes to campaigns are done

type Campaigns struct {
	sync.RWMutex
	Map map[string]Campaign
}
type Campaign struct {
	Id          int64
	PageWelcome string
	Hash        string
	Link        string
	Content     []byte
}

func (campaign Campaign) Serve(c *gin.Context) {
	m.Overall++
	m.Acess++
	utils.ServeBytes(campaign.Content, c)
}

func Reload() (err error) {
	camp.campaigns.Lock()
	defer camp.campaigns.Unlock()

	log.WithFields(log.Fields{}).Debug("campaigns reload...")
	begin := time.Now()
	defer func(err error) {
		errStr := ""
		if err != nil {
			errStr = err.Error()
		}
		log.WithFields(log.Fields{
			"error": errStr,
			"took":  time.Since(begin),
		}).Debug("campaigns reload")
	}(err)

	query := fmt.Sprintf("select id, link, hash, page_welcome from %scampaigns where status = $1",
		camp.dbConf.TablePrefix)

	var rows *sql.Rows
	rows, err = camp.dbConn.Query(query, ACTIVE_STATUS)
	if err != nil {
		err = fmt.Errorf("db.Query: %s, query: %s", err.Error(), query)
		return
	}
	defer rows.Close()

	var records []Campaign
	for rows.Next() {
		record := Campaign{}

		if err = rows.Scan(
			&record.Id,
			&record.Link,
			&record.Hash,
			&record.PageWelcome,
		); err != nil {
			err = fmt.Errorf("rows.Scan: %s", err.Error())
			return
		}
		filePath := camp.staticPath + "campaign/" + record.Hash + "/" + record.PageWelcome + ".html"
		record.Content, err = ioutil.ReadFile(filePath)
		if err != nil {
			m.LoadCampaignError.Set(1.)
			err := fmt.Errorf("ioutil.ReadFile: %s", err.Error())
			log.WithField("error", err.Error()).Error("ioutil.ReadFile serve file error")
			err = nil
		}
		log.WithField("path", filePath).Debug("loaded")

		records = append(records, record)
	}
	if rows.Err() != nil {
		err = fmt.Errorf("rows.Err: %s", err.Error())
		return
	}

	camp.campaigns.Map = make(map[string]Campaign, len(records))
	for _, campaign := range records {
		camp.campaigns.Map[campaign.Link] = campaign
	}
	return nil
}
