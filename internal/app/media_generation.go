package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
)

type mediaGenerationService interface {
	GenerateImage(context.Context, media.GenerateRequest) media.GenerateResult
}

func (h *sessionHandler) generateImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	service, ok := h.service.(mediaGenerationService)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input media.GenerateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Validate() != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	result := service.GenerateImage(r.Context(), input)
	if result.Validate() != nil {
		http.Error(w, `{"error":"media_result_unavailable"}`, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
