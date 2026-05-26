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
	QUICListenHost     = os.Getenv("HTTP3_LISTEN_HOST")
	TLSCertPath        = os.Getenv("TLS_CERT_PATH")
	TLSKeyPath         = os.Getenv("TLS_KEY_PATH")
)

func main() {
	var wg sync.WaitGroup

	http.HandleFunc("/", HeaderEcho)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	})

	var handler http.Handler

	quic_server := http3.Server{
		Addr:    QUICListenHost,
		Handler: handler,
	}

	https_server := http.Server{
		Addr:    SecureListenHost,
		Handler: handler,
	}

	https_server.Protocols = new(http.Protocols)
	https_server.Protocols.SetHTTP1(true)

	http_server := http.Server{
		Addr:    InsecureListenHost,
		Handler: handler,
	}

	http_server.Protocols = new(http.Protocols)
	http_server.Protocols.SetHTTP1(true)
	http_server.Protocols.SetUnencryptedHTTP2(true)

	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor < 3 {
			err := quic_server.SetQUICHeaders(w.Header())

			if err != nil {
				fmt.Println("error:", err)
			}
		}
		http.DefaultServeMux.ServeHTTP(w, r)
	})

	wg.Add(1)
	go func() {
		log.Printf("Starting secure server at %v", https_server.Addr)
		defer wg.Done()
		if err := https_server.ListenAndServeTLS(TLSCertPath, TLSKeyPath); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Add(1)
	go func() {
		log.Printf("Starting QUIC server at %v", quic_server.Addr)
		defer wg.Done()
		if err := quic_server.ListenAndServeTLS(TLSCertPath, TLSKeyPath); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Add(1)
	go func() {
		log.Printf("Starting insecure server at %v", http_server.Addr)
		defer wg.Done()
		if err := http_server.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Wait()
}
