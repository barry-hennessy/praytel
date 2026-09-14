package receiver

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"

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

func readProtobuf(req *http.Request, msg proto.Message) error {
	defer req.Body.Close()

	var r io.Reader = io.LimitReader(req.Body, maxOTLPMessageBytes+1)
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
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
