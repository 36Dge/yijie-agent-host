package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

type videoGenerationService interface {
	GenerateVideo(context.Context, wire.GenerateRequest) wire.VideoResult
	GetVideoResult(context.Context, wire.GetRequest) wire.VideoResult
}

func registerVideoGeneration(mux *http.ServeMux, h *sessionHandler) {
	mux.Handle("POST "+wire.GeneratePath, h.authorize(http.HandlerFunc(h.generateVideo)))
	mux.Handle("POST "+wire.GetPath, h.authorize(http.HandlerFunc(h.getVideoResult)))
}
func decodeVideoRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, wire.MaxRequestBytes))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return false
	}
	return true
}
func writeVideoResult(w http.ResponseWriter, result wire.VideoResult) {
	if result.Validate() != nil {
		http.Error(w, `{"error":"video_result_unavailable"}`, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
func (h *sessionHandler) generateVideo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	service, ok := h.service.(videoGenerationService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input wire.GenerateRequest
	if !decodeVideoRequest(w, r, &input) {
		return
	}
	writeVideoResult(w, service.GenerateVideo(r.Context(), input))
}
func (h *sessionHandler) getVideoResult(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	service, ok := h.service.(videoGenerationService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input wire.GetRequest
	if !decodeVideoRequest(w, r, &input) {
		return
	}
	writeVideoResult(w, service.GetVideoResult(r.Context(), input))
}
