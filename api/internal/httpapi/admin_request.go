package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const maxAdminBody = 4096

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeAdminJSONLimit(w, r, destination, maxAdminBody)
}

func decodeAdminJSONLimit(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func emptyAdminBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
	body, err := io.ReadAll(r.Body)
	return err == nil && len(body) == 0
}
