package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/yz1qaq/asterflow/crossborder/internal/service"
)

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(
		http.MaxBytesReader(w, r.Body, 1<<20),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
		return false
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "request body must contain one JSON value",
		})
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}

	status := http.StatusInternalServerError
	message := "internal server error"

	switch {
	case errors.Is(err, service.ErrInvalidInput):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, service.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	}

	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
