package operator

import (
	"database/sql"
	"errors"
	"net"
	"strings"

	log "github.com/Sirupsen/logrus"

	"github.com/vostrok/utils/db"
)

var op *operator

type OperatorConfig struct {
	Private []IpRange `yaml:"private_networks"`
}

type operator struct {
	db     *sql.DB
	conf   OperatorConfig
	dbConf db.DataBaseConfig

	privateIPRanges []IpRange
}

type IPInfo struct {
	IP            string
	CountryCode   int64
	OperatorCode  int64
	MsisdnHeaders []string
	Supported     bool
	Local         bool
	Range         IpRange
}

func Init(privateNetworks []IpRange, dbConfig db.DataBaseConfig) {
	log.SetLevel(log.DebugLevel)

	op = &operator{
		db:     db.Init(dbConfig),
		conf:   OperatorConfig{Private: privateNetworks},
		dbConf: dbConfig,
	}
	if err := reloadIPRanges(); err != nil {
		log.WithField("error", err.Error()).Fatal("Load IP ranges failed")
	}
	if err := memOperators.Reload(); err != nil {
		log.WithField("error", err.Error()).Fatal("operators load failed")
	}
	if err := memPrefixes.Reload(); err != nil {
		log.WithField("error", err.Error()).Fatal("prefixes reload failed")
	}

	op.loadPrivateNetworks(privateNetworks)
}
func GetSupported(infos []IPInfo) (IPInfo, error) {
	for _, v := range infos {
		if !v.Supported {
			continue
		}
		return v, nil
	}
	return IPInfo{}, errors.New("No any supported found")
}
func GetIpInfo(ipAddresses []net.IP) []IPInfo {
	infos := []IPInfo{}

	for _, ip := range ipAddresses {
		info := IPInfo{IP: ip.String(), Supported: false}

		if IsPrivateSubnet(ip) {
			info.Local = true
			log.WithFields(log.Fields{
				"info":  info.IP,
				"from ": info.Range.IpFrom,
				"to":    info.Range.IpTo,
			}).Debug("found local ip info")

			infos = append(infos, info)
			continue
		}

		for _, ipRange := range memIpRanges {
			if ipRange.In(ip) {
				info.Range = ipRange
				info.OperatorCode = ipRange.OperatorCode
				info.CountryCode = ipRange.CountryCode
				info.MsisdnHeaders = ipRange.MsisdnHeaders
				if info.OperatorCode != 0 {
					info.Supported = true
				}
			}
		}
		log.WithFields(log.Fields{
			"info":         info.IP,
			"from":         info.Range.IpFrom,
			"to":           info.Range.IpTo,
			"supported":    info.Supported,
			"operatorCode": info.OperatorCode,
		}).Debug("found ip info")

		infos = append(infos, info)
	}
	return infos
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
func GetInfoByMsisdn(msisdn string) (IPInfo, error) {
	info := IPInfo{}
	for prefix, operatorCode := range memPrefixes.Map {
		if strings.HasPrefix(msisdn, prefix) {
			info.Supported = true
			info.OperatorCode = operatorCode

			if ipRange, ok := memInfoByOperatorCode.Map[operatorCode]; ok {
				info.OperatorCode = ipRange.OperatorCode
				info.CountryCode = ipRange.CountryCode
				if operatorCode != 0 {
					info.Supported = true
				}
			}
			return info, nil
		}
	}
	return info, errors.New("Not found")
}
