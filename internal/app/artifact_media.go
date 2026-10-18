package app

import (
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/artifactmedia"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"io"
	"net/http"
)

type artifactMediaService interface {
	PublishArtifact(context.Context, wire.PublishRequest) (wire.PublishReceipt, error)
	SubscribeMediaEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

func (h *sessionHandler) mediaEvents(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(artifactMediaService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	for key := range r.URL.Query() {
		if key != "event_schema_version" && key != "after" && key != "stream_id" {
			http.Error(w, "invalid query", 400)
			return
		}
	}
	values := r.URL.Query()["event_schema_version"]
	if len(values) != 1 || values[0] != "9" {
		http.Error(w, "explicit schema 9 required", 400)
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "9")
	h.streamEvents(w, r, service.SubscribeMediaEvents)
}
func (h *sessionHandler) publishArtifact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	service, ok := h.service.(artifactMediaService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	var request wire.PublishRequest
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
		return
	}
	result, err := service.PublishArtifact(r.Context(), request)
	if err != nil {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"artifact_registration_unavailable"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
