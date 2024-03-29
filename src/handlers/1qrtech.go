package handlers

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	m "github.com/linkit360/go-dispatcherd/src/metrics"
	"github.com/linkit360/go-dispatcherd/src/rbmq"
	"github.com/linkit360/go-dispatcherd/src/sessions"
	mid_client "github.com/linkit360/go-mid/rpcclient"
	"github.com/linkit360/go-utils/rec"
	xmp_api_structs "github.com/linkit360/xmp-api/src/structs"
)

func AddQRTechHandlers() {
	if cnf.Service.LandingPages.QRTech.Enabled {
		e.Group("/lp/:campaign_link", AccessHandler).GET("", qrTechHandler)
		log.WithFields(log.Fields{}).Debug("qrtech handlers init")
	}
}

func qrTechHandler(c *gin.Context) {
	var err error
	m.Incoming.Inc()

	msg := gatherInfo(c)
	action := rbmq.UserActionsNotify{
		Action: "access",
		Tid:    msg.Tid,
	}
	logCtx := log.WithFields(log.Fields{
		"tid": msg.Tid,
	})

	defer func() {
		action.Msisdn = msg.Msisdn
		action.CampaignId = msg.CampaignId
		action.Tid = msg.Tid
		if err != nil {
			m.Errors.Inc()
			action.Error = err.Error()
			msg.Error = msg.Error + " " + err.Error()

			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("serve campaign")
		} else {
			m.CampaignAccess.Inc()
			m.Success.Inc()

			logCtx.WithFields(log.Fields{}).Info("serve ok")
		}

		if errAction := notifierService.ActionNotify(action); errAction != nil {
			logCtx.WithFields(log.Fields{
				"error":  errAction.Error(),
				"action": fmt.Sprintf("%#v", action),
			}).Error("notify user action")
		}

		if errAccessCampaign := notifierService.AccessCampaignNotify(msg); errAccessCampaign != nil {
			logCtx.WithFields(log.Fields{
				"error": errAccessCampaign.Error(),
				"msg":   fmt.Sprintf("%#v", msg),
			}).Error("notify access campaign")
		}

		val, ok := c.GetQuery("aff_sub")
		if ok && len(val) >= 5 {
			log.WithFields(log.Fields{
				"tid": msg.Tid,
			}).Debug("found pixel in get params")
			if err := notifierService.PixelBufferNotify(rec.Record{
				SentAt:     time.Now().UTC(),
				CampaignId: msg.CampaignId,
				Tid:        msg.Tid,
				Pixel:      val,
			}); err != nil {
				logCtx.WithFields(log.Fields{
					"error": err.Error(),
				}).Error("send pixel")
			}
		}
	}()

	paths := strings.Split(c.Request.URL.Path, "/")
	campaignLink := paths[len(paths)-1]

	campaign, ok := campaignByLink[campaignLink]
	if !ok {
		m.Errors.Inc()
		m.PageNotFoundError.Inc()
		err = fmt.Errorf("page not found: %s", campaignLink)

		log.WithFields(log.Fields{
			"error": err.Error(),
		}).Error("cannot get campaign by link")
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	msg.CampaignId = campaign.Id
	msg.ServiceCode = campaign.ServiceCode
	msg.CampaignHash = campaign.Hash
	msg.CountryCode = cnf.Service.LandingPages.QRTech.CountryCode
	if msg.IP == "" {
		m.IPNotFoundError.Inc()
	}
	if !msg.Supported {
		m.NotSupported.Inc()
	}

	msg.CampaignId = campaign.Id
	msg.ServiceCode = campaign.ServiceCode

	v := url.Values{}
	v.Add("SHORTCODE", campaign.ServiceCode)
	v.Add("SP_CONTENT", cnf.Service.LandingPages.QRTech.ContentUrl+"/get")

	telco, _ := c.GetQuery("telco")
	telco = strings.ToLower(telco)
	telco = strings.TrimSuffix(telco, "/")
	if telco == "dtac" {
		msg.OperatorCode = cnf.Service.LandingPages.QRTech.DtacOperatorCode
	} else if telco == "ais" {
		msg.OperatorCode = cnf.Service.LandingPages.QRTech.AisOperatorCode
	} else {
		logCtx.WithFields(log.Fields{
			"telco": telco,
		}).Error("unknown telco")
		err = fmt.Errorf("Unknown telco: %s", telco)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}

	if campaign.AutoClickEnabled {
		logCtx.WithFields(log.Fields{
			"telco": telco,
		}).Debug("autoclick enabled")
		var service xmp_api_structs.Service
		service, err = mid_client.GetServiceByCode(campaign.ServiceCode)
		if err != nil {
			err = fmt.Errorf("mid_client.GetServiceById: %s", err.Error())
			logCtx.WithFields(log.Fields{
				"error":      err.Error(),
				"service_id": msg.ServiceCode,
			}).Error("cannot get service by id")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		r := rec.Record{
			Msisdn:             msg.Msisdn,
			Tid:                msg.Tid,
			SubscriptionStatus: "",
			CampaignId:         campaign.Id,
			ServiceCode:        campaign.ServiceCode,
			CountryCode:        msg.CountryCode,
			OperatorCode:       msg.OperatorCode,
			Publisher:          sessions.GetFromSession("publisher", c),
			Pixel:              sessions.GetFromSession("pixel", c),
			DelayHours:         service.DelayHours,
			PaidHours:          service.PaidHours,
			RetryDays:          service.RetryDays,
			Price:              service.PriceCents,
		}

		contentUrl := cnf.Service.LandingPages.QRTech.ContentUrl + "get"

		encVars := url.Values{}
		encVars.Add("SHORTCODE", campaign.ServiceCode)
		encVars.Add("SP_CONTENT", contentUrl)
		telcoUrl := ""
		if strings.Contains(telco, "dtac") {
			telcoUrl = cnf.Service.LandingPages.QRTech.DtacUrl
		} else if strings.Contains(telco, "ais") {
			telcoUrl = cnf.Service.LandingPages.QRTech.AisUrl
		} else {
			err = fmt.Errorf("wrong telco: %s", telco)
			logCtx.WithFields(log.Fields{
				"serviceId": r.ServiceCode,
				"error":     err.Error(),
			}).Error("cannot redirect to autoclick")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}

		telcoUrl = telcoUrl + "?" + encVars.Encode()
		var req *http.Request
		req, err = http.NewRequest("GET", telcoUrl, nil)
		if err != nil {
			err = fmt.Errorf("Cann't create request: %s", err.Error())
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		httpClient := http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: time.Duration(cnf.Service.LandingPages.Beeline.Timeout) * time.Second,
		}

		var resp *http.Response
		resp, err = httpClient.Do(req)
		if err != nil {
			err = fmt.Errorf("Cann't make request: %s", err.Error())
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		defer resp.Body.Close()

		if telco == "dtac" {
			msg.UrlPath = resp.Header.Get("Location")
		} else {
			qrTechResponse, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				err = fmt.Errorf("ioutil.ReadAll: %s", err.Error())
				http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
				return
			}
			start := strings.Index(string(qrTechResponse), "url=http") + 4
			if start < 0 {
				err = fmt.Errorf("cannot parse response start: %s", string(qrTechResponse))
				http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
				return
			}
			end := strings.Index(string(qrTechResponse), `">`)
			if end < 0 {
				err = fmt.Errorf("cannot parse response end: %s", string(qrTechResponse))
				http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
				return
			}
			x := string(qrTechResponse)
			parsedUrl := x[start:end]
			msg.UrlPath = parsedUrl
		}

		logCtx.WithFields(log.Fields{
			"reqUrl":     telcoUrl,
			"contentUrl": contentUrl,
			"location":   msg.UrlPath,
		}).Debug("send to autoclick")

		if len(msg.UrlPath) == 0 {
			err = fmt.Errorf("no location in headers%s", "")
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("cannot get location")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		var telcoUrlEncrypted string
		telcoUrlEncrypted, err = cbcEncrypt([]byte(msg.UrlPath))
		if err != nil {
			err = fmt.Errorf("encrypt: %s", err.Error())
			logCtx.WithFields(log.Fields{
				"error": err.Error(),
			}).Error("cannot encrypt url")
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}

		autoClickV := url.Values{}
		autoClickV.Add("telco", telco)
		autoClickV.Add("url", telcoUrlEncrypted)
		reqUrl := cnf.Service.LandingPages.QRTech.AutoclickUrl + "?" + autoClickV.Encode()
		logCtx.WithFields(log.Fields{
			"telcoUrl":  msg.UrlPath,
			"encrypted": telcoUrlEncrypted,
			"reqUrl":    reqUrl,
		}).Debug("send to autoclick")

		http.Redirect(c.Writer, c.Request, reqUrl, 303)
		return
	}
	reqUrl := ""
	if telco == "dtac" || msg.OperatorCode == int64(52005) { // dtac
		reqUrl = cnf.Service.LandingPages.QRTech.DtacUrl + "?" + v.Encode()
		log.WithFields(log.Fields{
			"operator": "dtac",
			"url":      reqUrl,
		}).Debug("call")
		http.Redirect(c.Writer, c.Request, reqUrl, 303)
		return
	}

	if telco == "ais" || msg.OperatorCode == int64(52001) { // ais
		reqUrl = cnf.Service.LandingPages.QRTech.AisUrl + "?" + v.Encode()
		log.WithFields(log.Fields{
			"operator": "ais",
			"url":      reqUrl,
		}).Info("determined")
	} else {
		log.WithFields(log.Fields{
			"error": "cannot determine operator",
		}).Error("cannot determine operator")
		reqUrl = cnf.Service.LandingPages.QRTech.AisUrl + "?" + v.Encode()
	}

	var req *http.Request
	req, err = http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		err = fmt.Errorf("Cann't create request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	req.Close = false
	httpClient := http.Client{
		Timeout: time.Duration(cnf.Service.LandingPages.QRTech.Timeout) * time.Second,
	}
	var resp *http.Response
	resp, err = httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("Cann't make request: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
	}
	if resp.StatusCode > 220 {
		err = fmt.Errorf("qrTech resp status: %s", resp.Status)
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	var qrTechResponse []byte
	qrTechResponse, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("ioutil.ReadAll: %s", err.Error())
		http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
		return
	}
	defer resp.Body.Close()

	logCtx.WithFields(log.Fields{
		"response": string(qrTechResponse),
		"len":      len(string(qrTechResponse)),
	}).Debug("got response")

	if telco == "ais" {
		start := strings.Index(string(qrTechResponse), "url=http") + 4
		if start < 0 {
			err = fmt.Errorf("cannot parse response start: %s", string(qrTechResponse))
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		end := strings.Index(string(qrTechResponse), `">`)
		if end < 0 {
			err = fmt.Errorf("cannot parse response end: %s", string(qrTechResponse))
			http.Redirect(c.Writer, c.Request, cnf.Service.ErrorRedirectUrl, 303)
			return
		}
		x := string(qrTechResponse)
		parsedUrl := x[start:end]
		http.Redirect(c.Writer, c.Request, parsedUrl, 303)
		return
	}
}

func cbcEncrypt(plaintext []byte) (res string, err error) {
	key := []byte(cnf.Service.LandingPages.QRTech.AesKey)
	plaintext = padding(plaintext, aes.BlockSize)

	// CBC mode works on blocks so plaintexts may need to be padded to the
	// next whole block. For an example of such padding, see
	// https://tools.ietf.org/html/rfc5246#section-6.2.3.2. Here we'll
	// assume that the plaintext is already of the correct length.
	//slice := make([]byte, len(plaintext)+aes.BlockSize-len(plaintext)%aes.BlockSize)
	//copy(slice, plaintext)

	// CBC mode works on blocks so plaintexts may need to be padded to the
	// next whole block. For an example of such padding, see
	// https://tools.ietf.org/html/rfc5246#section-6.2.3.2. Here we'll
	// assume that the plaintext is already of the correct length.
	if len(plaintext)%aes.BlockSize != 0 {
		panic("plaintext is not a multiple of the block size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	// The IV needs to be unique, but not secure. Therefore it's common to
	// include it at the beginning of the ciphertext.
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[aes.BlockSize:], plaintext)

	// It's important to remember that ciphertexts must be authenticated
	// (i.e. by using crypto/hmac) as well as being encrypted in order to
	// be secure.

	return hex.EncodeToString(ciphertext), nil
}

func padding(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padtext...)
}
