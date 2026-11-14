package app

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/skillmarket"
	"github.com/google/uuid"
)

type NativeSkillMarket interface {
	Catalog(context.Context) (wire.Catalog, error)
	Mutate(context.Context, wire.Mutation) (wire.Operation, error)
	Import(context.Context, wire.ImportRequest, []byte) (wire.Operation, error)
	Operation(context.Context, string) (wire.Operation, error)
	Cancel(context.Context, string) (wire.Operation, error)
}

func WithNativeSkillMarket(service NativeSkillMarket) HandlerOption {
	return func(options *handlerOptions) { options.nativeSkillMarket = service }
}
func registerNativeSkillRoutes(mux *http.ServeMux, config SkillFeatureConfig, service NativeSkillMarket, token string) {
	authorize := func(manage bool, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !security.TokenMatches(token, r.Header.Get("Authorization")) {
				writeJSON(w, 401, wire.ErrorResponse{Code: "unauthorized"})
				return
			}
			if !config.Read || manage && !config.Manage {
				writeJSON(w, 403, wire.ErrorResponse{Code: "capability_denied"})
				return
			}
			if !config.NativeMarketEnabled || service == nil {
				writeJSON(w, 503, wire.ErrorResponse{Code: "skill_market_unavailable", Retryable: true})
				return
			}
			next(w, r)
		}
	}
	response := func(w http.ResponseWriter, value any, err error) {
		if err != nil {
			code := skillmarket.ErrorCode(err)
			status := 503
			switch code {
			case "invalid_request", "import_too_large", "import_format_invalid", "import_archive_invalid", "import_path_invalid", "import_layout_invalid", "import_entrypoint_missing", "import_multiple_skills", "import_plugin_unsupported", "import_path_collision", "import_entry_invalid":
				status = 400
			case "operation_conflict", "skill_epoch_conflict", "skill_busy", "reconciliation_required", "skill_not_offered":
				status = 409
			case "skill_not_found":
				status = 404
			}
			writeJSON(w, status, wire.ErrorResponse{Code: code, Retryable: status == 503})
			return
		}
		writeJSON(w, 200, value)
	}
	validID := func(r *http.Request) bool {
		id, err := uuid.Parse(r.PathValue("operation_id"))
		return err == nil && id != uuid.Nil && id.String() == r.PathValue("operation_id")
	}
	mux.HandleFunc("GET /v2/skills", authorize(false, func(w http.ResponseWriter, r *http.Request) {
		out, err := service.Catalog(r.Context())
		response(w, out, err)
	}))
	mux.HandleFunc("POST /v2/skills/operations", authorize(true, func(w http.ResponseWriter, r *http.Request) {
		var request wire.Mutation
		if err := decodeRequest(w, r, &request); err != nil {
			writeJSON(w, 400, wire.ErrorResponse{Code: "invalid_request"})
			return
		}
		out, err := service.Mutate(r.Context(), request)
		response(w, out, err)
	}))
	mux.HandleFunc("POST /v2/skills/imports/{operation_id}", authorize(true, func(w http.ResponseWriter, r *http.Request) {
		filename := r.URL.Query().Get("filename")
		format := "markdown"
		if strings.EqualFold(filepath.Ext(filename), ".zip") {
			format = "zip"
		}
		request := wire.ImportRequest{OperationId: r.PathValue("operation_id"), FileName: filename, Format: format, Sha256: r.Header.Get("X-Skill-SHA256"), Bytes: r.ContentLength}
		if r.Header.Get("Content-Type") != "application/octet-stream" || request.Validate() != nil || !validID(r) {
			writeJSON(w, 400, wire.ErrorResponse{Code: "invalid_request"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, skillmarket.MaxImportBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, 400, wire.ErrorResponse{Code: "import_too_large"})
			return
		}
		out, err := service.Import(r.Context(), request, data)
		response(w, out, err)
	}))
	mux.HandleFunc("GET /v2/skills/operations/{operation_id}", authorize(false, func(w http.ResponseWriter, r *http.Request) {
		if !validID(r) {
			writeJSON(w, 400, wire.ErrorResponse{Code: "invalid_request"})
			return
		}
		out, err := service.Operation(r.Context(), r.PathValue("operation_id"))
		response(w, out, err)
	}))
	mux.HandleFunc("POST /v2/skills/operations/{operation_id}/cancel", authorize(true, func(w http.ResponseWriter, r *http.Request) {
		if !validID(r) {
			writeJSON(w, 400, wire.ErrorResponse{Code: "invalid_request"})
			return
		}
		out, err := service.Cancel(r.Context(), r.PathValue("operation_id"))
		response(w, out, err)
	}))
}
