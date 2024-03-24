package httpclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	url2 "net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ssgo/log"
	"github.com/ssgo/standard"
	"github.com/ssgo/u"
	"golang.org/x/net/http2"
)

type ClientPool struct {
	pool             *http.Client
	GlobalHeaders    map[string]string
	NoBody           bool
	Debug            bool
	DownloadPartSize int64
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
	return &ClientPool{pool: clientConfig, GlobalHeaders: map[string]string{}, DownloadPartSize: 4194304}
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
	}, GlobalHeaders: map[string]string{}, DownloadPartSize: 4194304}
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
func (cp *ClientPool) Head(url string, headers ...string) *Result {
	return cp.Do("HEAD", url, nil, headers...)
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
		xForwardFor = ", " + xForwardFor
	}
	xForwardFor = request.RemoteAddr[0:strings.IndexByte(request.RemoteAddr, ':')] + xForwardFor
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
	return cp.do(true, method, url, data, headers...)
}

func (cp *ClientPool) ManualDo(method, url string, data interface{}, headers ...string) *Result {
	return cp.do(false, method, url, data, headers...)
}

type Range struct {
	Start int64
	End   int64
}

func downloadPart(cp *ClientPool, fp *os.File, task *Range, url string, headers ...string) (int64, error) {
	headers[len(headers)-1] = fmt.Sprintf("bytes=%d-%d", task.Start, task.End)
	r := cp.ManualDo("GET", url, nil, headers...)
	if r.Error != nil {
		return 0, r.Error
	} else {
		n, err := io.Copy(fp, r.Response.Body)
		return n, err
	}
}

func (cp *ClientPool) Download(filename, url string, callback func(start, end int64, ok bool, finished, total int64), headers ...string) (*Result, error) {
	r1 := cp.Head(url, headers...)
	total := r1.Response.ContentLength
	tasks := make([]Range, 0)
	if total > 0 {
		n := int64(0)
		for i := int64(0); i < total; i += cp.DownloadPartSize {
			tasks = append(tasks, Range{i, i + cp.DownloadPartSize - 1})
			n = i + cp.DownloadPartSize
		}
		if n < total {
			tasks = append(tasks, Range{n, total})
		}

		u.CheckPath(filename)
		fp, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err == nil {
			defer fp.Close()
			finished := int64(0)
			headers = append(headers, "Range", "")
			retryTask := make([]Range, 0)
			for _, task := range tasks {
				n, err := downloadPart(cp, fp, &task, url, headers...)
				finished += n
				if callback != nil {
					callback(task.Start, task.End, err == nil, finished, total)
				}
				if err != nil {
					retryTask = append(retryTask, task)
				}
			}
			retry2Task := make([]Range, 0)
			for _, task := range retryTask {
				n, err := downloadPart(cp, fp, &task, url, headers...)
				finished += n
				if callback != nil {
					callback(task.Start, task.End, err == nil, finished, total)
				}
				if err != nil {
					retry2Task = append(retry2Task, task)
				}
			}
			for _, task := range retry2Task {
				n, err := downloadPart(cp, fp, &task, url, headers...)
				finished += n
				if callback != nil {
					callback(task.Start, task.End, err == nil, finished, total)
				}
			}
			if finished < total {
				return nil, errors.New("download file failed")
			}
		} else {
			return nil, err
		}
		return r1, nil
	} else {
		r := cp.ManualDo("GET", url, nil, headers...)
		if r.Error != nil {
			return r, r.Error
		}
		defer r.Response.Body.Close()
		fp, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err == nil {
			defer fp.Close()
			_, err := io.Copy(fp, r.Response.Body)
			if err != nil {
				return r, err
			}
		}
		return r, nil
	}
}

func (cp *ClientPool) MPost(url string, formData map[string]string, files map[string]interface{}, headers ...string) (*Result, []error) {
	errors := make([]error, 0)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if formData != nil {
		for k, v := range formData {
			if err := writer.WriteField(k, v); err != nil {
				errors = append(errors, err)
			}
		}
	}

	if files != nil {
		for k, v := range files {
			if filename, ok := v.(string); ok && u.FileExists(filename) {
				if fp, err := os.Open(filename); err == nil {
					if part, err := writer.CreateFormFile(k, filepath.Base(filename)); err == nil {
						if _, err = io.Copy(part, fp); err != nil {
							errors = append(errors, err)
						}
					} else {
						errors = append(errors, err)
					}
					_ = fp.Close()
				} else {
					errors = append(errors, err)
				}
			} else {
				h := make(textproto.MIMEHeader)
				var buf []byte
				if reader, ok := v.(io.Reader); ok {
					buf, _ = io.ReadAll(reader)
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, k, k))
					h.Set("Content-Type", "application/octet-stream")
				} else if bytesV, ok := v.([]byte); ok {
					buf = bytesV
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, k, k))
					h.Set("Content-Type", "application/octet-stream")
				} else if strV, ok := v.(string); ok {
					buf = []byte(strV)
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s.txt"`, k, k))
					h.Set("Content-Type", "text/plain")
				} else {
					buf = u.JsonBytesP(v)
					h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s.json"`, k, k))
					h.Set("Content-Type", "application/json")
				}

				if part, err := writer.CreatePart(h); err == nil {
					if _, err = part.Write(buf); err != nil {
						errors = append(errors, err)
					}
				} else {
					errors = append(errors, err)
				}

			}
		}
	}

	err := writer.Close()
	if err != nil {
		errors = append(errors, err)
	}

	if len(errors) == 0 {
		headers = append(headers, "Content-Type", writer.FormDataContentType())
		r := cp.Post(url, body, headers...)
		if r.Error != nil {
			errors = append(errors, r.Error)
		}
		return r, errors
	}

	return nil, errors
}

func (cp *ClientPool) do(fetchBody bool, method, url string, data interface{}, headers ...string) *Result {
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
		case io.Reader:
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
			fmt.Println("reader 0")
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

	if !fetchBody || cp.NoBody {
		return &Result{data: nil, Response: res}
	} else {
		defer res.Body.Close()
		result, err := io.ReadAll(res.Body)
		if err != nil {
			return &Result{Error: err}
		}
		if cp.Debug {
			log.DefaultLogger.Info("http response data", "url", req.RequestURI, "data", result)
		}
		return &Result{data: result, Response: res}
	}
}

func (cp *ClientPool) getRealIp(request *http.Request) string {
	return u.StringIf(request.Header.Get(standard.DiscoverHeaderClientIp) != "", request.Header.Get(standard.DiscoverHeaderClientIp), request.RemoteAddr[0:strings.IndexByte(request.RemoteAddr, ':')])
}

func (rs *Result) Save(filename string) error {
	u.CheckPath(filename)
	if dstFile, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600); err == nil {
		defer dstFile.Close()
		if rs.data == nil {
			defer rs.Response.Body.Close()
			io.Copy(dstFile, rs.Response.Body)
		} else {
			dstFile.Write(rs.data)
		}
		return nil
	} else {
		return err
	}
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
