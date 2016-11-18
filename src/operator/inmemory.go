package operator

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
)

// reload all:
// operator_ip
// operators
// operator_msisdn_prefix
func Reload() error {
	if err := reloadIPRanges(); err != nil {
		log.WithField("error", err.Error()).Fatal("Load IP ranges failed")
		return err
	}
	if err := memOperators.Reload(); err != nil {
		log.WithField("error", err.Error()).Fatal("repoad operators info failed")
		return err
	}
	if err := memPrefixes.Reload(); err != nil {
		log.WithField("error", err.Error()).Fatal("prefixes reload failed")
		return err
	}
	return nil
}

// Tasks:
// Keep in memory all active operator prefixes
// Reload when changes to prefixes are done
var memPrefixes = &Prefixes{}

type Prefixes struct {
	sync.RWMutex
	Map map[string]int64
}

type prefix struct {
	Prefix       string
	OperatorCode int64
}

func (pp *Prefixes) Reload() error {
	var err error
	pp.Lock()
	defer pp.Unlock()

	log.WithFields(log.Fields{}).Debug("operator msisdn prefixes reload...")
	begin := time.Now()
	defer func() {
		fields := log.Fields{
			"took": time.Since(begin),
		}
		if err != nil {
			fields["error"] = err.Error()
		}
		log.WithFields(fields).Debug("operator msisdn prefixes reload")
	}()

	query := fmt.Sprintf("SELECT "+
		"operator_code, "+
		"prefix "+
		"FROM %soperator_msisdn_prefix",
		op.dbConf.TablePrefix)

	var rows *sql.Rows
	rows, err = op.db.Query(query)
	if err != nil {
		err = fmt.Errorf("db.Query: %s, query: %s", err.Error(), query)
		return err
	}
	defer rows.Close()

	var prefixes []prefix
	for rows.Next() {
		var p prefix
		if err = rows.Scan(&p.OperatorCode, &p.Prefix); err != nil {
			err = fmt.Errorf("rows.Scan: %s", err.Error())
			return err
		}
		prefixes = append(prefixes, p)
	}
	if rows.Err() != nil {
		err = fmt.Errorf("rows.Err: %s", err.Error())
		return err
	}

	pp.Map = make(map[string]int64, len(prefixes))
	for _, p := range prefixes {
		pp.Map[p.Prefix] = p.OperatorCode
	}
	return nil
}

// in-memory IP ranges
var memIpRanges = []IpRange{}

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

var memInfoByOperatorCode = &InfoByOperatorCode{}

type InfoByOperatorCode struct {
	sync.RWMutex
	Map map[int64]*IpRange
}

// msisdn could be in many headers
func reloadIPRanges() (err error) {
	log.WithFields(log.Fields{}).Debug("operators ip ranges reload...")
	begin := time.Now()
	defer func(err error) {
		fields := log.Fields{
			"took": time.Since(begin),
		}
		if err != nil {
			fields["error"] = err.Error()
		}
		log.WithFields(fields).Debug("operators ip ranges reload")
	}(err)

	query := fmt.Sprintf(""+
		"SELECT "+
		"id, "+
		"operator_code, "+
		"country_code, "+
		"ip_from, "+
		"ip_to, "+
		" ( SELECT %soperators.msisdn_headers as header "+
		"FROM %soperators "+
		"WHERE operator_code = code ) "+
		" from %soperator_ip",
		op.dbConf.TablePrefix,
		op.dbConf.TablePrefix,
		op.dbConf.TablePrefix,
	)
	var rows *sql.Rows
	rows, err = op.db.Query(query)
	if err != nil {
		err = fmt.Errorf("db.Query: %s, query: %s", err.Error(), query)
		return
	}
	defer rows.Close()

	var records []IpRange
	for rows.Next() {
		record := IpRange{}

		var headers string
		if err = rows.Scan(
			&record.Id,
			&record.OperatorCode,
			&record.CountryCode,
			&record.IpFrom,
			&record.IpTo,
			&headers,
		); err != nil {
			err = fmt.Errorf("rows.Scan: %s", err.Error())
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
		record.Start = net.ParseIP(record.IpFrom)
		record.End = net.ParseIP(record.IpTo)
		records = append(records, record)
	}
	if rows.Err() != nil {
		err = fmt.Errorf("rows.Err: %s", err.Error())
		return
	}
	memIpRanges = records

	memInfoByOperatorCode.Map = make(map[int64]*IpRange)
	for _, v := range records {
		memInfoByOperatorCode.Map[v.OperatorCode] = &v
	}

	log.WithFields(log.Fields{
		"IpRangesLen": len(memIpRanges),
		"IpRanges":    memIpRanges,
	}).Info("IpRanges loaded")
	return nil
}

// Tasks:
// Keep in memory all operators names and configuration
// Reload when changes to operators table are done
// todo: same code in mt_manager

var memOperators = &Operators{}

type Operators struct {
	sync.RWMutex
	ByCode map[int64]*Operator
}

type Operator struct {
	Name     string
	Rps      int
	Settings string
	Code     int64
}

func GetOperatorNameByCode(code int64) string {
	if operator, ok := memOperators.ByCode[code]; ok {
		return strings.ToLower(operator.Name)
	}
	return ""
}

func (ops *Operators) Reload() error {
	ops.Lock()
	defer ops.Unlock()

	var err error
	log.WithFields(log.Fields{}).Debug("operators reload...")
	begin := time.Now()
	defer func() {

		for k, v := range ops.ByCode {
			log.Debug(fmt.Sprintf("%d %#v", k, v))
		}

		fields := log.Fields{
			"ops":  fmt.Sprintf("%#v", ops.ByCode),
			"took": time.Since(begin),
		}
		if err != nil {
			fields["error"] = err.Error()
		}
		log.WithFields(fields).Debug("operators reload")
	}()

	query := fmt.Sprintf("SELECT "+
		"name, "+
		"code,  "+
		"rps, "+
		"settings "+
		"FROM %soperators",
		op.dbConf.TablePrefix)
	var rows *sql.Rows
	rows, err = op.db.Query(query)
	if err != nil {
		err = fmt.Errorf("db.Query: %s, query: %s", err.Error(), query)
		return err
	}
	defer rows.Close()

	var operators []Operator
	for rows.Next() {
		var operator Operator
		if err = rows.Scan(
			&operator.Name,
			&operator.Code,
			&operator.Rps,
			&operator.Settings,
		); err != nil {
			err = fmt.Errorf("rows.Scan: %s", err.Error())
			return err
		}
		log.Debug(fmt.Sprintf("%#v", operator))
		operators = append(operators, operator)
	}
	if rows.Err() != nil {
		err = fmt.Errorf("rows.Err: %s", err.Error())
		return err
	}

	ops.ByCode = make(map[int64]*Operator, len(operators))
	for _, op := range operators {
		ops.ByCode[op.Code] = &op
	}
	return nil
}
