package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/go-chi/traceid"
	"github.com/golang-cz/devslog"
	"github.com/quic-go/quic-go/http3"
)

type RequestInfo struct {
	RequestID  string
	Hostname   string
	URL        string
	RemoteAddr string
	Protocol   string
	Headers    http.Header
}

func JSONInfo(w http.ResponseWriter, r *http.Request) {
	var info RequestInfo

	info.URL = r.URL.String()
	info.RemoteAddr = r.RemoteAddr
	info.RequestID = middleware.GetReqID(r.Context())
	info.Protocol = r.Proto
	info.Headers = r.Header

	w.Header().Add("Content-Type", "application/json;charset=utf-8")

	j := json.NewEncoder(w)
	j.Encode(info)
}

func PlainInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain;charset=utf-8")
	fmt.Fprintf(w, "RequestID: %s\n", middleware.GetReqID(r.Context()))
	fmt.Fprintf(w, "Protocol: %s\n", r.RemoteAddr)
	fmt.Fprintf(w, "RemoteAddr: %s\n", r.RemoteAddr)
	r.Write(w)
}

func GoSource(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "&http.Request{\n")

	field := func(name string, value any) {
		fmt.Fprintf(w, "  %-11s %#v\n", name+":", value)
	}

	field("Host", r.Host)
	field("URL", r.URL.String())
	field("RemoteAddr", r.RemoteAddr)
	field("Proto", r.Proto)

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

func logHandler(isLocalhost bool, handlerOpts *slog.HandlerOptions) slog.Handler {
	if isLocalhost {
		// Pretty logs for localhost development.
		return devslog.NewHandler(os.Stdout, &devslog.Options{
			SortKeys:           true,
			MaxErrorStackTrace: 5,
			MaxSlicePrintSize:  20,
			HandlerOptions:     handlerOpts,
		})
	}

	// JSON logs for production with "traceId".
	return traceid.LogHandler(
		slog.NewJSONHandler(os.Stdout, handlerOpts),
	)
}

func main() {
	isLocalhost := os.Getenv("ENV") == "localhost"
	logFormat := httplog.SchemaECS.Concise(isLocalhost)

	logger := slog.New(logHandler(isLocalhost, &slog.HandlerOptions{
		AddSource:   !isLocalhost,
		ReplaceAttr: logFormat.ReplaceAttr,
	}))

	slog.SetDefault(logger)
	slog.SetLogLoggerLevel(slog.LevelError)

	var wg sync.WaitGroup

	var quic_server http3.Server
	var http_server http.Server
	var https_server http.Server

	r := chi.NewRouter()
	r.Use(traceid.Middleware)

	// Request logger.
	r.Use(httplog.RequestLogger(logger, &httplog.Options{
		// Level defines the verbosity of the request logs:
		// slog.LevelDebug - log all responses (incl. OPTIONS)
		// slog.LevelInfo  - log all responses (excl. OPTIONS)
		// slog.LevelWarn  - log 4xx and 5xx responses only (except for 429)
		// slog.LevelError - log 5xx responses only
		Level: slog.LevelInfo,

		// Log attributes using given schema/format.
		Schema: logFormat,

		// RecoverPanics recovers from panics occurring in the underlying HTTP handlers
		// and middlewares. It returns HTTP 500 unless response status was already set.
		//
		// NOTE: Panics are logged as errors automatically, regardless of this setting.
		RecoverPanics: true,

		// Filter out some request logs.
		Skip: func(req *http.Request, respStatus int) bool {
			return respStatus == 404 || respStatus == 405
		},

		// Select request/response headers to be logged explicitly.
		LogRequestHeaders:  []string{"Host"},
		LogResponseHeaders: []string{},

		// Log all requests with invalid payload as curl command.
		LogExtraAttrs: func(req *http.Request, reqBody string, respStatus int) []slog.Attr {
			if respStatus == 400 || respStatus == 422 {
				req.Header.Del("Authorization")
				return []slog.Attr{slog.String("curl", httplog.CURL(req, reqBody))}
			}
			return nil
		},
	}))
	r.Use(func(h http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor < 3 {
				err := quic_server.SetQUICHeaders(w.Header())

				if err != nil {
					fmt.Println("error:", err)
				}
			}
			h.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	})
	r.Use(middleware.Heartbeat("/ping"))
	r.Use(middleware.Compress(5))
	r.Use(middleware.RequestID)
	r.Use(middleware.GetHead)

	r.HandleFunc("/", PlainInfo)
	r.HandleFunc("/go", GoSource)
	r.HandleFunc("/go/*", GoSource)
	r.HandleFunc("/json", JSONInfo)
	r.HandleFunc("/json/*", JSONInfo)

	quic_server.Addr = QUICListenHost
	quic_server.Handler = r
	quic_server.Logger = logger

	https_server.Addr = SecureListenHost
	https_server.Handler = r
	https_server.Protocols = new(http.Protocols)
	https_server.Protocols.SetHTTP1(true)
	https_server.Protocols.SetHTTP2(true)

	http_server.Addr = InsecureListenHost
	http_server.Handler = r
	http_server.Protocols = new(http.Protocols)
	http_server.Protocols.SetHTTP1(true)
	http_server.Protocols.SetHTTP2(true)
	http_server.Protocols.SetUnencryptedHTTP2(true)

	wg.Add(1)
	go func() {
		logger.Info("Starting HTTPS server", "addr", https_server.Addr)
		defer wg.Done()
		if err := https_server.ListenAndServeTLS(TLSCertPath, TLSKeyPath); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Add(1)
	go func() {
		// log.Printf("Starting QUIC server at %v", quic_server.Addr)
		logger.Info("Starting QUIC server", "addr", quic_server.Addr)
		defer wg.Done()
		if err := quic_server.ListenAndServeTLS(TLSCertPath, TLSKeyPath); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Add(1)
	go func() {
		logger.Info("Starting Plain HTTP server", "addr", http_server.Addr)
		defer wg.Done()
		if err := http_server.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	wg.Wait()
}
