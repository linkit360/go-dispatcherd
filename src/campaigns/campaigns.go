package campaigns

import (
	"database/sql"
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/gin-gonic/gin"

	"github.com/vostrok/db"
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
	log.SetLevel(log.DebugLevel)

	camp = &campaign{
		dbConn:     db.Init(conf),
		dbConf:     conf,
		staticPath: static,
		campaigns:  &Campaigns{},
	}

	err := Reload()
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Load IP ranges fail")
	}
}

// Tasks:
// Keep in memory all active campaigns
// Allow to get a campaign information by campaign id fastly
// Reload when changes to campaigns are done

type Campaigns struct {
	sync.RWMutex
	Map map[int64]Campaign
}
type Campaign struct {
	Id          int64
	PageWelcome string
	Hash        string
	Link        string
}

func (campaign Campaign) Serve(c *gin.Context) {
	utils.ServeFile(camp.staticPath+"/"+campaign.Hash+"/"+campaign.PageWelcome, c)
}

func Reload() error {
	query := fmt.Sprintf("select id, link, hash, page_welcome from %scampaigns where status = $1",
		camp.dbConf.TablePrefix)
	rows, err := camp.dbConn.Query(query, ACTIVE_STATUS)
	if err != nil {
		return fmt.Errorf("QueryServices: %s, query: %s", err.Error(), query)
	}
	defer rows.Close()

	var records []Campaign
	for rows.Next() {
		record := Campaign{}

		if err := rows.Scan(
			&record.Id,
			&record.Link,
			&record.Hash,
			&record.PageWelcome,
		); err != nil {
			return err
		}
		records = append(records, record)
	}
	if rows.Err() != nil {
		return fmt.Errorf("RowsError: %s", err.Error())
	}

	camp.campaigns.Lock()
	defer camp.campaigns.Unlock()

	camp.campaigns.Map = make(map[int64]Campaign, len(records))
	for _, campaign := range records {
		camp.campaigns.Map[campaign.Id] = campaign
	}
	return nil
}
