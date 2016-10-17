package operator

import (
	"bytes"
	"database/sql"
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"
)

var op operator

type Operator interface {
	IsSupported(msisdn string) bool
}

type OperatorConfig struct {
	DbConf  DataBaseConfig `yaml:"db"`
	Private []IpRange      `yaml:"private_networks"`
}

func Init(conf OperatorConfig) {
	op.initDatabase(conf.DbConf)
	err := op.loadIPRanges()
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Load IP ranges fail")
	}
	op.loadPrivateNetworks(conf.Private)
}

type operator struct {
	dbConfig        DataBaseConfig
	db              *sql.DB
	ipRanges        []IpRange
	privateIPRanges []IpRange
}

type IPInfo struct {
	IP           string
	CountryCode  int64
	OperatorCode int64
	Supported    bool
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
			if info.OperatorCode != 0 {
				info.Supported = true
			}
			return info
		}
	}
	return info
}

type RPCReloadOperatorsIP struct{}
type ReloadOperatorsIPRequest struct{}
type ReloadOperatorsIPResponse struct {
	Success bool
}

func (rpc *RPCReloadOperatorsIP) ReloadOperatorsIP(req ReloadOperatorsIPRequest, res *ReloadOperatorsIPResponse) error {
	err := op.loadIPRanges()
	if err != nil {
		res.Success = false
		log.WithField("error", err.Error()).Error("Load IP ranges fail")
		return err
	}
	res.Success = true
	return nil
}

// todo - rewrite in binary three
func (op operator) loadIPRanges() (err error) {

	query := "SELECT id, operator_code, country_code, ip_from, ip_to from xmp_operator_ip"
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
	return nil
}
func (op operator) loadPrivateNetworks(ipConf []IpRange) {
	op.privateIPRanges = []IpRange{}
	for _, v := range ipConf {
		v.Start = net.ParseIP(v.IpFrom)
		v.End = net.ParseIP(v.IpTo)
		op.privateIPRanges = append(op.privateIPRanges, v)
	}

}

type IpRange struct {
	Id           int64  `json:"id,omitempty" yaml:"-"`
	OperatorCode int64  `json:"operator_code,omitempty" yaml:"-"`
	CountryCode  int64  `json:"country_code,omitempty" yaml:"-"`
	IpFrom       string `json:"ip_from,omitempty" yaml:"start"`
	Start        net.IP `json:"-" yaml:"-"`
	IpTo         string `json:"ip_to,omitempty" yaml:"end"`
	End          net.IP `json:"-" yaml:"-"`
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
