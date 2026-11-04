package receiver

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/barry-hennessy/bones/observability"
	"google.golang.org/protobuf/proto"
)

var errOTLPBodyTooLarge = errors.New("otlp http body too large")

func requirePOST(w http.ResponseWriter, req *http.Request) bool {
	if req.Method == http.MethodPost {
		return true
	}

	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

	return false
}

func limitHTTPBody(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxOTLPMessageBytes)
}

func readProtobuf(ctx context.Context, lt observability.LoggingTracer, req *http.Request, msg proto.Message) error {
	_, log, span := lt.Name("receiver.readProtobuf").Start(ctx)
	defer span.End()

	defer req.Body.Close()

	r := io.LimitReader(req.Body, maxOTLPMessageBytes+1)
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			log.Warn("reading gzip body", "err", err)
			return err
		}
		defer gz.Close()

		r = io.LimitReader(gz, maxOTLPMessageBytes+1)
	}

	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if int64(len(body)) > maxOTLPMessageBytes {
		log.Warn("otlp body too large", "limit", maxOTLPMessageBytes)
		return errOTLPBodyTooLarge
	}

	return proto.Unmarshal(body, msg)
}

func writeProtobuf(w http.ResponseWriter, msg proto.Message) {
	body, err := proto.Marshal(msg)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func limitHTTPInflight(next http.Handler, n uint32) http.Handler {
	if n == 0 {
		return next
	}

	sem := make(chan struct{}, n)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, req)
		default:
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		}
	})
}
