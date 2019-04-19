package httpclient

import (
	"github.com/ssgo/log"
	"github.com/ssgo/standard"
	"golang.org/x/net/http2"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func T1estHttp(tt *testing.T) {
	c := GetClient(time.Second)
	r := c.Get("http://61.135.169.121")
	if r.Error != nil || strings.Index(r.String(), "baidu.com") == -1 {
		tt.Error("baidu error	", r.Error)
	}
}

func TestStream(tt *testing.T) {
	c := GetClient(time.Second)
	c.NoBody = true
	r := c.Get("http://61.135.169.121")

	if r.Error != nil || strings.Index(r.String(), "baidu.com") != -1 {
		tt.Error("baidu error	", r.Error)
	}
}

func T1estH2C(tt *testing.T) {
	listener, _ := net.Listen("tcp", ":20080")
	startChan := make(chan bool, 1)
	go start(listener, startChan)
	<-startChan
	//time.Sleep(500*time.Millisecond)

	c := GetClientH2C(time.Second)
	r := c.Get("http://localhost:20080")
	if r.Error != nil {
		tt.Error("h2c error	", r.Error)
	}
	if r.String() != "Hello" {
		tt.Error("h2c result err	", r.String())
	}

	listener.Close()
}

func start(l net.Listener, startChan chan bool) {
	server := http.Server{}
	http2.VerboseLogs = true
	server.Addr = ":20080"

	s2 := &http2.Server{
		IdleTimeout: 1 * time.Minute,
	}
	http2.ConfigureServer(&server, s2)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
		log.LogRequest("testApp", server.Addr, r.RemoteAddr, r.Header.Get(standard.DiscoverHeaderFromApp), r.Header.Get(standard.DiscoverHeaderFromNode), r.Header.Get(standard.DiscoverHeaderClientId), r.Header.Get(standard.DiscoverHeaderSessionId), r.Header.Get(standard.DiscoverHeaderRequestId), r.Header.Get(standard.DiscoverHeaderHost), r.Header.Get(standard.DiscoverHeaderScheme), r.Proto, 0, 0, r.Method, r.RequestURI, getHeaders(r.Header), nil, 0.00032, 200, getHeaders(w.Header()), 5, "Hello", nil)
	})

	startChan <- true
	for {
		rwc, err := l.Accept()
		if err != nil {
			continue
		}
		go s2.ServeConn(rwc, &http2.ServeConnOpts{BaseConfig: &server})
	}
}

func getHeaders(headers http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		if len(v) > 1 {
			out[k] = strings.Join(v, "; ")
		} else {
			out[k] = v[0]
		}
	}
	return out
}
