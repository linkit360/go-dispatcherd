package operator

import (
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"time"

	log "github.com/Sirupsen/logrus"
)

type DataBaseConfig struct {
	ConnMaxLifetime  int    `default:"-1" yaml:"conn_ttl"`
	MaxOpenConns     int    `default:"15" yaml:"max_conns"`
	ReconnectTimeout int    `default:"10" yaml:"timeout"`
	User             string `default:""`
	Pass             string `default:""`
	Port             string `default:""`
	Name             string `default:""`
	Host             string `default:""`
	SSLMode          string `default:"disable" yaml:"ssl_mode"`
	TablePrefix      string `default:"" yaml:"table_prefix"`
}

func (dbConfig DataBaseConfig) GetConnStr() string {
	dsn := "postgres://" +
		dbConfig.User + ":" +
		dbConfig.Pass + "@" +
		dbConfig.Host + ":" +
		dbConfig.Port + "/" +
		dbConfig.Name + "?sslmode=" +
		dbConfig.SSLMode
	return dsn
}

func (o *operator) initDatabase(dbConf DataBaseConfig) {
	o.dbConfig = dbConf
	o.connect()
}

func (o *operator) connect() {

	if o.db != nil {
		return
	}

	var err error
	o.db, err = sql.Open("postgres", o.dbConfig.GetConnStr())
	if err != nil {
		fmt.Printf("open error %s, dsn: %s", err.Error(), o.dbConfig.GetConnStr())
		log.WithField("error", err.Error()).Fatal("db connect")
	}
	if err = o.db.Ping(); err != nil {
		fmt.Printf("ping error %s, dsn: %s", err.Error(), o.dbConfig.GetConnStr())
		log.WithField("error", err.Error()).Fatal("db ping")
	}

	o.db.SetMaxOpenConns(o.dbConfig.MaxOpenConns)
	o.db.SetConnMaxLifetime(time.Second * time.Duration(o.dbConfig.ConnMaxLifetime))

	log.WithFields(log.Fields{
		"host": o.dbConfig.Host, "dbname": o.dbConfig.Name, "user": o.dbConfig.User}).Debug("database connected")
	return
}
