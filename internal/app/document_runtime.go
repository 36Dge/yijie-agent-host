package app

import (
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"io"
	"net/http"
)

type documentRuntimeService interface {
	DocumentEnvironment(context.Context, wire.EnvironmentRequest) (wire.EnvironmentResponse, error)
	DocumentArtifactStatus(context.Context, wire.StatusRequest) (wire.StatusResponse, error)
}

func (h *sessionHandler) documentEnvironment(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(documentRuntimeService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input wire.EnvironmentRequest
	if !readDocumentRequest(w, r, &input) {
		return
	}
	result, err := service.DocumentEnvironment(r.Context(), input)
	writeDocumentResult(w, result, err)
}
func (h *sessionHandler) documentArtifactStatus(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(documentRuntimeService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input wire.StatusRequest
	if !readDocumentRequest(w, r, &input) {
		return
	}
	result, err := service.DocumentArtifactStatus(r.Context(), input)
	writeDocumentResult(w, result, err)
}
func readDocumentRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, `{"error":"invalid_request"}`, 400)
		return false
	}
	return true
}
func writeDocumentResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		http.Error(w, `{"error":"document_capability_unavailable"}`, 409)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
