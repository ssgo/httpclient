package httpclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/cookiejar"
	url2 "net/url"
	"reflect"
	"strings"
	"time"

	"github.com/ssgo/log"
	"github.com/ssgo/standard"
	"github.com/ssgo/u"
	"golang.org/x/net/http2"
)

type ClientPool struct {
	pool          *http.Client
	GlobalHeaders map[string]string
	NoBody        bool
	Debug         bool
}

type Result struct {
	Error    error
	Response *http.Response
	data     []byte
}

type Form = map[string]string

func GetClientH2C(timeout time.Duration) *ClientPool {
	if timeout < time.Millisecond {
		timeout *= time.Millisecond
	}
	jar, _ := cookiejar.New(nil)
	clientConfig := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: timeout,
		Jar:     jar,
	}
	return &ClientPool{pool: clientConfig, GlobalHeaders: map[string]string{}}
}
func GetClient(timeout time.Duration) *ClientPool {
	if timeout < time.Millisecond {
		timeout *= time.Millisecond
	}
	jar, _ := cookiejar.New(nil)
	return &ClientPool{pool: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar: jar,
	}, GlobalHeaders: map[string]string{}}
}

func (cp *ClientPool) EnableRedirect() {
	cp.pool.CheckRedirect = nil
}
func (cp *ClientPool) SetGlobalHeader(k, v string) {
	if v == "" {
		delete(cp.GlobalHeaders, k)
	} else {
		cp.GlobalHeaders[k] = v
	}
}

func (cp *ClientPool) Get(url string, headers ...string) *Result {
	return cp.Do("GET", url, nil, headers...)
}
func (cp *ClientPool) Post(url string, data interface{}, headers ...string) *Result {
	return cp.Do("POST", url, data, headers...)
}
func (cp *ClientPool) Put(url string, data interface{}, headers ...string) *Result {
	return cp.Do("PUT", url, data, headers...)
}
func (cp *ClientPool) Delete(url string, data interface{}, headers ...string) *Result {
	return cp.Do("DELETE", url, data, headers...)
}
func (cp *ClientPool) Head(url string, data interface{}, headers ...string) *Result {
	return cp.Do("HEAD", url, data, headers...)
}
func (cp *ClientPool) DoByRequest(request *http.Request, method, url string, data interface{}, settedHeaders ...string) *Result {
	headers := map[string]string{}
	// 注释掉不续传未在standard.DiscoverRelayHeaders中定义的头
	//for k, v := range request.Header {
	//	headers[k] = v[0]
	//}

	// 续传 X-...
	for _, h := range standard.DiscoverRelayHeaders {
		if request.Header.Get(h) != "" {
			headers[h] = request.Header.Get(h)
		}
	}

	// 续传 X-Forward-For
	xForwardFor := request.Header.Get(standard.DiscoverHeaderForwardedFor)
	if xForwardFor != "" {
		xForwardFor = ", "+xForwardFor
	}
	xForwardFor = request.RemoteAddr[0:strings.IndexByte(request.RemoteAddr, ':')]+xForwardFor
	headers[standard.DiscoverHeaderForwardedFor] = xForwardFor

	//// 真实的用户IP，通过 X-Real-IP 续传
	//headers = append(headers, standard.DiscoverHeaderClientIp, cp.getRealIp(request))
	//
	//// 客户端IP列表，通过 X-Forwarded-For 接力续传
	//headers = append(headers, standard.DiscoverHeaderForwardedFor, request.Header.Get(standard.DiscoverHeaderForwardedFor)+u.StringIf(request.Header.Get(standard.DiscoverHeaderForwardedFor) == "", "", ", ")+request.RemoteAddr[0:strings.IndexByte(request.RemoteAddr, ':')])
	//
	//// 客户唯一编号，通过 X-Client-ID 续传
	//if request.Header.Get(standard.DiscoverHeaderClientId) != "" {
	//	headers = append(headers, standard.DiscoverHeaderClientId, request.Header.Get(standard.DiscoverHeaderClientId))
	//}
	//
	//// 会话唯一编号，通过 X-Session-ID 续传
	//if request.Header.Get(standard.DiscoverHeaderSessionId) != "" {
	//	headers = append(headers, standard.DiscoverHeaderSessionId, request.Header.Get(standard.DiscoverHeaderSessionId))
	//}
	//
	//// 请求唯一编号，通过 X-Request-ID 续传
	//requestId := request.Header.Get(standard.DiscoverHeaderRequestId)
	//if requestId == "" {
	//	requestId = u.UniqueId()
	//	request.Header.Set(standard.DiscoverHeaderRequestId, requestId)
	//}
	//headers = append(headers, standard.DiscoverHeaderRequestId, requestId)
	//
	//// 真实用户请求的Host，通过 X-Host 续传
	//host := request.Header.Get(standard.DiscoverHeaderHost)
	//if host == "" {
	//	host = request.Host
	//	request.Header.Set(standard.DiscoverHeaderHost, host)
	//}
	//headers = append(headers, standard.DiscoverHeaderHost, host)
	//
	//// 真实用户请求的Scheme，通过 X-Scheme 续传
	//scheme := request.Header.Get(standard.DiscoverHeaderScheme)
	//if scheme == "" {
	//	scheme = u.StringIf(request.TLS == nil, "http", "https")
	//	request.Header.Set(standard.DiscoverHeaderScheme, scheme)
	//}
	//headers = append(headers, standard.DiscoverHeaderScheme, scheme)

	for i := 1; i < len(settedHeaders); i += 2 {
		headers[settedHeaders[i-1]] = settedHeaders[i]
	}

	headerArgs := make([]string, 0)
	for k, v := range headers {
		headerArgs = append(headerArgs, k, v)
	}
	return cp.Do(method, url, data, headerArgs...)
}
func (cp *ClientPool) Do(method, url string, data interface{}, headers ...string) *Result {
	var req *http.Request
	var err error
	contentType := ""
	contentLength := 0
	if data == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		//var bytesData []byte
		err = nil
		var reader io.Reader
		//fmt.Println("  000", reflect.TypeOf(data))

		switch t := data.(type) {
		case *io.ReadCloser:
			reader = *t
		case io.ReadCloser:
			reader = t
		case []byte:
			reader = bytes.NewReader(t)
		case string:
			reader = bytes.NewReader([]byte(t))
		case url2.Values:
			reader = bytes.NewReader([]byte(t.Encode()))
			contentType = "application/x-www-form-urlencoded"
		case map[string][]string:
			formData := url2.Values{}
			for k, values := range t {
				for _, v := range values {
					formData.Add(k, v)
				}
			}
			bytesData := []byte(formData.Encode())
			reader = bytes.NewReader(bytesData)
			contentType = "application/x-www-form-urlencoded"
			contentLength = len(bytesData)
		case map[string]string:
			formData := url2.Values{}
			for k, v := range t {
				formData.Set(k, v)
			}
			bytesData := []byte(formData.Encode())
			reader = bytes.NewReader(bytesData)
			contentType = "application/x-www-form-urlencoded"
			contentLength = len(bytesData)
		default:
			//bytesData, err = json.MarshalIndent(data, "", "  ")
			//fmt.Println("  111", data)
			bytesData := u.JsonBytes(data)
			//fmt.Println("  222", string(bytesData))
			reader = bytes.NewReader(bytesData)
			contentType = "application/json"
			contentLength = len(bytesData)
		}
		if err == nil {
			req, err = http.NewRequest(method, url, reader)
			if contentType != "" {
				req.Header.Set("Content-Type", contentType)
			}
			if contentLength > 0 {
				req.Header.Set("Content-Length", u.String(contentLength))
			}
		}
	}
	if err != nil {
		return &Result{Error: err}
	}
	for i := 1; i < len(headers); i += 2 {
		if headers[i-1] == "Host" {
			req.Host = headers[i]
		} else if req.Header.Get(headers[i-1]) == "" {
			req.Header.Set(headers[i-1], headers[i])
		}
	}
	for k, v := range cp.GlobalHeaders {
		req.Header.Set(k, v)
	}
	if cp.Debug {
		log.DefaultLogger.Info("http request", "method", req.Method, "host", req.Host, "path", req.URL.Path, "headers", req.Header, "data", data)
	}
	res, err := cp.pool.Do(req)
	if cp.Debug {
		log.DefaultLogger.Info("http response", "headers", res.Header, "err", err)
	}
	if err != nil {
		return &Result{Error: err}
	}
	if res.ContentLength == -1 {
		res.ContentLength = u.Int64(res.Header.Get("Content-Length"))
	}

	if cp.NoBody {
		return &Result{data: nil, Response: res}
	} else {
		result, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return &Result{Error: err}
		}
		_ = res.Body.Close()
		if cp.Debug {
			log.DefaultLogger.Info("http response data", "url", req.RequestURI, "data", result)
		}
		return &Result{data: result, Response: res}
	}
}

func (cp *ClientPool) getRealIp(request *http.Request) string {
	return u.StringIf(request.Header.Get(standard.DiscoverHeaderClientIp) != "", request.Header.Get(standard.DiscoverHeaderClientIp), request.RemoteAddr[0:strings.IndexByte(request.RemoteAddr, ':')])
}

func (rs *Result) String() string {
	if rs.data == nil {
		return ""
	}
	return string(rs.data)
}

func (rs *Result) Bytes() []byte {
	return rs.data
}

func (rs *Result) Map() map[string]interface{} {
	tr := make(map[string]interface{})
	_ = rs.To(&tr)
	return tr
}

func (rs *Result) Arr() []interface{} {
	tr := make([]interface{}, 0)
	_ = rs.To(&tr)
	return tr
}

func (rs *Result) ToAction(result interface{}) string {
	var actionStart = -1
	var actionEnd = -1
	var resultStart = -1
	var resultEnd = -1
	for i, c := range rs.data {
		if actionStart == -1 && c == '"' {
			actionStart = i
		}
		if actionEnd == -1 && actionStart != -1 && c == '"' {
			actionEnd = i
		}
		if resultStart == -1 && actionEnd != -1 && c != '\r' && c != '\n' && c != '\t' && c != ' ' && c != ',' {
			resultStart = i
			break
		}
	}

	if resultStart != -1 {
		for i := len(rs.data); i >= 0; i-- {
			c := rs.data[i]
			if c != '\r' && c != '\n' && c != '\t' && c != ' ' && c != ']' {
				resultEnd = i
				break
			}
		}
	}

	if resultEnd == -1 || resultEnd <= resultStart {
		return ""
	}

	_ = convertBytesToObject(rs.data[resultStart:resultEnd-resultStart], result)
	return string(rs.data[actionStart : actionEnd-actionStart])
}

func (rs *Result) To(result interface{}) error {
	return convertBytesToObject(rs.data, result)
}

func convertBytesToObject(data []byte, result interface{}) error {
	var err error = nil
	if data == nil {
		return fmt.Errorf("no result")
	}

	t := reflect.TypeOf(result)
	v := reflect.ValueOf(result)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		v = v.Elem()
	}
	if t.Kind() == reflect.Map || (t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8) {
		err = json.Unmarshal(data, result)
	} else if t.Kind() == reflect.Struct {
		tr := new(map[string]interface{})
		err = json.Unmarshal(data, tr)
		if err == nil {
			u.Convert(tr, result)
		}
	}
	return err
}
