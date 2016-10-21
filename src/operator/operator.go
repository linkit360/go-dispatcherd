package operator

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"

	"github.com/gin-gonic/gin"
	"github.com/vostrok/db"
)

var op operator

type Operator interface {
	IsSupported(msisdn string) bool
}

type OperatorConfig struct {
	DbConf  db.DataBaseConfig `yaml:"db"`
	Private []IpRange         `yaml:"private_networks"`
}

type operator struct {
	db              *sql.DB
	conf            OperatorConfig
	ipRanges        []IpRange
	privateIPRanges []IpRange
}

type IPInfo struct {
	IP           string
	CountryCode  int64
	OperatorCode int64
	Header       string
	Supported    bool
}

func Init(conf OperatorConfig) {
	log.SetLevel(log.DebugLevel)

	op = operator{}
	op.db = db.Init(conf.DbConf)
	op.conf = conf
	err := op.loadIPRanges()
	if err != nil {
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
			info.OperatorCode = ipRange.OperatorCode
			info.CountryCode = ipRange.CountryCode
			info.Header = ipRange.HeaderName
			if info.OperatorCode != 0 {
				info.Supported = true
			}
			return info
		}
	}
	return info
}

// todo - rewrite in binary three
// todo msisdn could be in many headers
func (op operator) loadIPRanges() (err error) {
	query := fmt.Sprintf(""+
		"SELECT id, operator_code, country_code, ip_from, ip_to, "+
		" ( SELECT %soperators.msisdn_header_name as header FROM %soperators where operator_code = code ) "+
		" from %soperator_ip", op.conf.DbConf.TablePrefix, op.conf.DbConf.TablePrefix, op.conf.DbConf.TablePrefix)
	rows, err := op.db.Query(query)
	if err != nil {
		return fmt.Errorf("GetIpRanges: %s, query: %s", err.Error(), query)
	}
	defer rows.Close()

	var records []IpRange
	for rows.Next() {
		record := IpRange{}

		if err := rows.Scan(
			&record.Id,
			&record.OperatorCode,
			&record.CountryCode,
			&record.IpFrom,
			&record.IpTo,
			&record.HeaderName,
		); err != nil {
			return err
		}
		record.Start = net.ParseIP(record.IpFrom)
		record.End = net.ParseIP(record.IpTo)
		records = append(records, record)
	}
	if rows.Err() != nil {
		return fmt.Errorf("GetIpRanges RowsError: %s", err.Error())
	}
	op.ipRanges = records
	log.WithField("IpRanges", len(op.ipRanges)).Info("IpRanges loaded")

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

type IpRange struct {
	Id           int64  `json:"id,omitempty" yaml:"-"`
	OperatorCode int64  `json:"operator_code,omitempty" yaml:"-"`
	CountryCode  int64  `json:"country_code,omitempty" yaml:"-"`
	IpFrom       string `json:"ip_from,omitempty" yaml:"start"`
	Start        net.IP `json:"-" yaml:"-"`
	IpTo         string `json:"ip_to,omitempty" yaml:"end"`
	End          net.IP `json:"-" yaml:"-"`
	HeaderName   string `json:"header_name" yaml:"-"`
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

type response struct {
	Success bool        `json:"success,omitempty"`
	Err     error       `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Status  int         `json:"-"`
}

func AddCQRHandlers(r *gin.Engine) {
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
		err := op.loadIPRanges()
		if err != nil {
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
