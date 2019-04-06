package httpclient

import (
	"golang.org/x/net/http2"
	"net"
	"net/http"
	"testing"
	"time"
)

func Hello() string {
	return "Hello"
}

func TestHttp(tt *testing.T) {
	c := GetClient(time.Second)
	r := c.Get("http://61.135.169.121")
	if r.Error != nil {
		tt.Error("baidu error	", r.Error)
	}
}
func TestH2C(tt *testing.T) {
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
