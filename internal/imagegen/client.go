package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ModeTextToImage   = "text_to_image"
	ModeSubject       = "subject_reference"
	maxPromptRunes    = 1500
	MaxImageBytes     = 20 << 20
	maxReferenceBytes = 10_000_000
	maxImageEdge      = 16_384
	maxImagePixels    = 40_000_000
	requestTimeout    = 120 * time.Second
)

const (
	ErrorInvalidRequest   = "invalid_request"
	ErrorAuthentication   = "authentication_failed"
	ErrorBalance          = "balance_insufficient"
	ErrorRateLimited      = "rate_limited"
	ErrorContentRejected  = "content_rejected"
	ErrorTimeout          = "provider_timeout"
	ErrorUnavailable      = "provider_unavailable"
	ErrorInvalidResponse  = "invalid_response"
	ErrorGenerationFailed = "generation_failed"
	ErrorRequestCanceled  = "request_canceled"
)

// Request is the closed Host-owned projection accepted by the image provider.
// ReferenceDataURL is never sourced from model tool arguments.
type Request struct {
	Prompt           string
	Mode             string
	AspectRatio      string
	ReferenceDataURL string
}

type Result struct {
	Bytes     []byte
	MediaType string
	Width     int
	Height    int
}

type Generator interface {
	Generate(context.Context, Request) (Result, error)
}

type ProviderError struct {
	Code       string
	HTTPStatus int
	VendorCode string
}

func (e *ProviderError) Error() string {
	return "image provider failed: " + e.Code
}

func ErrorCode(err error) string {
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError.Code
	}
	return ErrorGenerationFailed
}

// Diagnostic includes normalized status and only known provider error identifiers.
func Diagnostic(err error) string {
	var p *ProviderError
	if !errors.As(err, &p) {
		return "Image generation failed."
	}
	value := p.Code
	if p.HTTPStatus > 0 {
		value += " HTTP " + strconv.Itoa(p.HTTPStatus)
	}
	if p.VendorCode != "" {
		value += " " + p.VendorCode
	}
	return "Image generation: " + value + "."
}

func Retryable(err error) bool {
	switch ErrorCode(err) {
	case ErrorRateLimited, ErrorTimeout, ErrorUnavailable:
		return true
	default:
		return false
	}
}

func decodeImage(encoded string, maximum int) (Result, error) {
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(maximum) {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	content, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(content) == 0 || len(content) > maximum || base64.StdEncoding.EncodeToString(content) != encoded {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	return inspectImage(content)
}

func inspectImage(content []byte) (Result, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || !validDimensions(config.Width, config.Height) || (format != "png" && format != "jpeg") {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(content))
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	mediaType := "image/png"
	if format == "jpeg" {
		mediaType = "image/jpeg"
	}
	return Result{Bytes: content, MediaType: mediaType, Width: config.Width, Height: config.Height}, nil
}

func validateReferenceDataURL(dataURL string) error {
	var prefix string
	switch {
	case strings.HasPrefix(dataURL, "data:image/png;base64,"):
		prefix = "data:image/png;base64,"
	case strings.HasPrefix(dataURL, "data:image/jpeg;base64,"):
		prefix = "data:image/jpeg;base64,"
	default:
		return &ProviderError{Code: ErrorInvalidRequest}
	}
	encoded := strings.TrimPrefix(dataURL, prefix)
	result, err := decodeImage(encoded, maxReferenceBytes-1)
	if err != nil {
		return &ProviderError{Code: ErrorInvalidRequest}
	}
	if "data:"+result.MediaType+";base64," != prefix {
		return &ProviderError{Code: ErrorInvalidRequest}
	}
	return nil
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= maxImageEdge && height <= maxImageEdge &&
		int64(width) <= int64(maxImagePixels)/int64(height)
}

func httpFailureCode(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorAuthentication
	case http.StatusPaymentRequired:
		return ErrorBalance
	case http.StatusTooManyRequests:
		return ErrorRateLimited
	default:
		if status >= http.StatusInternalServerError {
			return ErrorUnavailable
		}
		return ErrorGenerationFailed
	}
}

func DisplayName(_ string) string {
	// Artifact identity is announced before the provider response reveals its
	// concrete media type. Keep the identity stable across started/completed
	// events; the MIME type remains authoritative for preview and save flows.
	return "generated-image"
}
