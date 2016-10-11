package content

import (
	"errors"
)

func GetHashBySubscriptionLink(h string) (s string, err error) {
	return "", errors.New("Not Implemented")
}

//
//func Content(w http.ResponseWriter, r *http.Request) {
//
//	rsp, err := ContentClient.Deliver(context.Background(), &content_proto.DeliverRequest{
//		Cookies:   lookup.Cookies(r),
//		Get:       lookup.Get(r),
//		Headers:   lookup.Headers(r),
//		Ip:        lookup.GeoIp(r),
//		Post:      lookup.Post(r),
//		Referer:   lookup.Referer(r),
//		Url:       lookup.Url(r),
//		Useragent: lookup.UserAgent(r),
//	})
//	if err != nil || rsp == nil {
//		log.Println("ContentClient.Deliver", err, rsp)
//		Render(w, r, 404, "not_found.html", err)
//		return
//	}
//
//	http.ServeFile(w, r, "/var/www/xmp.linkit360.ru/web/"+rsp.Object)
//}
