package operator

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"

	"github.com/vostrok/db"
)

var op *operator

type OperatorConfig struct {
	Private []IpRange `yaml:"private_networks"`
}

type operator struct {
	db              *sql.DB
	conf            OperatorConfig
	dbConf          db.DataBaseConfig
	ipRanges        []IpRange
	privateIPRanges []IpRange
}

type IPInfo struct {
	IP            string
	CountryCode   int64
	OperatorCode  int64
	MsisdnHeaders []string
	Supported     bool
	Range         IpRange
}

type IpRange struct {
	Id            int64    `json:"id,omitempty" yaml:"-"`
	OperatorCode  int64    `json:"operator_code,omitempty" yaml:"-"`
	CountryCode   int64    `json:"country_code,omitempty" yaml:"-"`
	IpFrom        string   `json:"ip_from,omitempty" yaml:"start"`
	Start         net.IP   `json:"-" yaml:"-"`
	IpTo          string   `json:"ip_to,omitempty" yaml:"end"`
	End           net.IP   `json:"-" yaml:"-"`
	MsisdnHeaders []string `yaml:"-"`
}

func (r IpRange) In(ip net.IP) bool {
	if ip.To4() == nil {
		return false
	}
	if bytes.Compare(ip, r.Start) >= 0 && bytes.Compare(ip, r.End) <= 0 {
		return true
	}
	return false
}

func Init(conf OperatorConfig, dbConfig db.DataBaseConfig) {
	log.SetLevel(log.DebugLevel)

	op = &operator{
		db:     db.Init(dbConfig),
		conf:   conf,
		dbConf: dbConfig,
	}
	if err := Reload(); err != nil {
		log.WithField("error", err.Error()).Fatal("Load IP ranges fail")
	}

	op.loadPrivateNetworks(conf.Private)
}

func GetIpInfo(ipAddr net.IP) IPInfo {
	info := IPInfo{IP: ipAddr.String(), Supported: false}

	if IsPrivateSubnet(ipAddr) {
		return info
	}
	for _, ipRange := range op.ipRanges {
		if ipRange.In(ipAddr) {
			info.Range = ipRange
			info.OperatorCode = ipRange.OperatorCode
			info.CountryCode = ipRange.CountryCode
			info.MsisdnHeaders = ipRange.MsisdnHeaders
			if info.OperatorCode != 0 {
				info.Supported = true
			}
			return info
		}
	}
	return info
}

// msisdn could be in many headers
// todo - rewrite in binary three
func Reload() (err error) {
	query := fmt.Sprintf(""+
		"SELECT id, operator_code, country_code, ip_from, ip_to, "+
		" ( SELECT %soperators.msisdn_headers as header FROM %soperators where operator_code = code ) "+
		" from %soperator_ip", op.dbConf.TablePrefix, op.dbConf.TablePrefix, op.dbConf.TablePrefix)
	rows, err := op.db.Query(query)
	if err != nil {
		return fmt.Errorf("GetIpRanges: %s, query: %s", err.Error(), query)
	}
	defer rows.Close()

	var records []IpRange
	for rows.Next() {
		record := IpRange{}

		var headers string
		if err := rows.Scan(
			&record.Id,
			&record.OperatorCode,
			&record.CountryCode,
			&record.IpFrom,
			&record.IpTo,
			&headers,
		); err != nil {
			return err
		}
		decodedHeaders := make([]string, 0)
		if err := json.Unmarshal([]byte(headers), &decodedHeaders); err != nil {
			log.WithFields(log.Fields{
				"error":   err.Error(),
				"iprange": record,
			}).Fatal("unmarshaling headers")
		}
		record.MsisdnHeaders = decodedHeaders
		log.WithFields(log.Fields{
			"operator":       record.OperatorCode,
			"headers":        headers,
			"decodedHeaders": fmt.Sprintf("%#v", decodedHeaders),
		}).Debug("unmarshaling headers")

		record.Start = net.ParseIP(record.IpFrom)
		record.End = net.ParseIP(record.IpTo)
		records = append(records, record)
	}
	if rows.Err() != nil {
		return fmt.Errorf("GetIpRanges RowsError: %s", err.Error())
	}
	op.ipRanges = records
	log.WithFields(log.Fields{
		"IpRangesLen": len(op.ipRanges),
		"IpRanges":    op.ipRanges,
	}).Info("IpRanges loaded")
	return nil
}
func (op operator) loadPrivateNetworks(ipConf []IpRange) {
	op.privateIPRanges = []IpRange{}
	for _, v := range ipConf {
		v.Start = net.ParseIP(v.IpFrom)
		v.End = net.ParseIP(v.IpTo)
		op.privateIPRanges = append(op.privateIPRanges, v)
	}
	log.WithField("privateNetworks", op.privateIPRanges).Info("private networks loaded")
}

func IsPrivateSubnet(ipAddress net.IP) bool {
	if ipCheck := ipAddress.To4(); ipCheck != nil {
		for _, r := range op.privateIPRanges {
			if r.In(ipAddress) {
				return true
			}
		}
	}
	return false
}
