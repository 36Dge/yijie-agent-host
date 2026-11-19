package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

type builtinWebControlService interface {
	BuiltinWebControl(context.Context, wire.WebControlRequest) (wire.WebControlResponse, error)
}
type webControlResponse struct{ wire.WebControlResponse }

func (v webControlResponse) Validate() error {
	b, e := json.Marshal(v.WebControlResponse)
	if e != nil {
		return e
	}
	return wire.Validate("WebControlResponse", b)
}
func registerBuiltinWeb(mux *http.ServeMux, service interface {
	AdmitBuiltinWeb(context.Context, wire.WebAdmissionRequest) (wire.WebAdmissionReceipt, error)
}, token string) {
	if service == nil || token == "" {
		return
	}
	mux.HandleFunc("POST /internal/web-retrieval/admit", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(403)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		defer r.Body.Close()
		if err != nil || len(raw) > 8192 || wire.Validate("WebAdmissionRequest", raw) != nil {
			w.WriteHeader(400)
			return
		}
		var q wire.WebAdmissionRequest
		_ = json.Unmarshal(raw, &q)
		if r.Header.Get("Accept") == "application/vnd.yijie.web-admission.v2+json" {
			if scoped, ok := service.(interface {
				AdmitBuiltinWebV2(context.Context, wire.WebAdmissionRequest) (wire.WebAdmissionReceiptV2, error)
			}); ok {
				result, e := scoped.AdmitBuiltinWebV2(r.Context(), q)
				if e != nil {
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "application/vnd.yijie.web-admission.v2+json")
				w.Header().Set("Cache-Control", "no-store")
				_ = json.NewEncoder(w).Encode(result)
				return
			}
		}
		receipt, err := service.AdmitBuiltinWeb(r.Context(), q)
		if err != nil {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(receipt)
	})
}
