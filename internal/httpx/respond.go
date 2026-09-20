package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// WriteJSON writes v as a JSON response body with the given status code.
// It is the single JSON responder for every control-plane HTTP handler.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteErr writes a JSON error envelope {"error": msg}. The web console reads
// the "error" key first (falling back to "message"), so this is the canonical
// error shape across the API.
func WriteErr(w http.ResponseWriter, code int, msg string) {
	WriteJSON(w, code, map[string]string{"error": msg})
}

// WriteErrOrInternal renders err as a client-safe response. Errors listed in
// safe are echoed verbatim with their mapped status (validation failures,
// sentinels like not-found / conflict that a caller acts on). Any other error
// is logged with the request path and returned as a generic 500, so internal
// detail (driver messages, wrapped causes, host paths) never reaches the client.
// r may be nil when called from a shared error mapper that has no request in
// scope; the failure is still logged, just without path and method.
func WriteErrOrInternal(w http.ResponseWriter, r *http.Request, err error, safe map[error]int) {
	if code, ok := safeStatus(err, safe); ok {
		WriteErr(w, code, err.Error())
		return
	}
	if r != nil {
		slog.Warn("request failed", "path", r.URL.Path, "method", r.Method, "err", err)
	} else {
		slog.Warn("request failed", "err", err)
	}
	WriteErr(w, http.StatusInternalServerError, "internal server error")
}

func safeStatus(err error, safe map[error]int) (int, bool) {
	for sentinel, code := range safe {
		if errors.Is(err, sentinel) {
			return code, true
		}
	}
	return 0, false
}
