package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/quic-go/quic-go/http3"
)

type RequestInfo struct {
	ServerHostname  string
	RemoteAddr      string
	Path            string
	RequestHeaders  http.Header
	ResponseHeaders http.Header
}

func HeaderEcho(w http.ResponseWriter, r *http.Request) {
	// var info RequestInfo

	// hostname, err := os.Hostname()
	// if err != nil {
	// 	w.WriteHeader(500)
	// 	fmt.Fprintf(w, "error: %s", err)
	// 	return
	// }

	fmt.Fprintf(w, "&http.Request{\n")

	field := func(name string, value any) {
		fmt.Fprintf(w, "  %-11s %#v\n", name+":", value)
	}

	field("Host", r.Host)
	field("URL", r.URL)
	field("RemoteAddr", r.RemoteAddr)
	field("Proto", r.Proto)
	field("TransferEncoding", r.TransferEncoding)

	fmt.Fprintf(w, "  %-11s %s\n", "Header: ", "http.Header{")
	headerLength := 0
	for name := range r.Header {
		headerLength = max(len(fmt.Sprintf("%#v:", name)), headerLength)
	}

	for header, values := range r.Header {
		fmt.Fprintf(w, "    %*s %#v,\n", -headerLength, fmt.Sprintf("%#v:", header), values)
	}
	fmt.Fprintf(w, "  },\n")
	fmt.Fprintf(w, "}\n")
}

var (
	InsecureListenHost = os.Getenv("HTTP_LISTEN_HOST")
	SecureListenHost   = os.Getenv("HTTPS_LISTEN_HOST")
	TLSCertPath        = os.Getenv("TLS_CERT_PATH")
	TLSKeyPath         = os.Getenv("TLS_KEY_PATH")
)

func main() {
	var wg sync.WaitGroup

	http.HandleFunc("/", HeaderEcho)
	wg.Add(1)
	go func() {
		log.Printf("Starting secure server at https://localhost%v", SecureListenHost)
		defer wg.Done()
		if err := http3.ListenAndServeTLS(SecureListenHost, TLSCertPath, TLSKeyPath, nil); err != nil {
			log.Fatal(err)
		}
	}()
	wg.Add(1)
	go func() {
		addr := InsecureListenHost
		log.Printf("Starting insecure server at http://localhost%v", addr)
		defer wg.Done()
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Wait()
}
