package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	Endpoint          = "https://api.minimaxi.com/v1/image_generation"
	Model             = "image-01"
	ModeTextToImage   = "text_to_image"
	ModeSubject       = "subject_reference"
	maxPromptRunes    = 1500
	MaxImageBytes     = 20 << 20
	maxReferenceBytes = 10_000_000
	maxImageEdge      = 16_384
	maxImagePixels    = 40_000_000
	requestTimeout    = 120 * time.Second
	responseSlack     = 1 << 20
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

var (
	aspectRatios = map[string]struct{}{
		"1:1": {}, "16:9": {}, "4:3": {}, "3:2": {},
		"2:3": {}, "3:4": {}, "9:16": {}, "21:9": {},
	}
	canonicalCount = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
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
	Code string
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

func Retryable(err error) bool {
	switch ErrorCode(err) {
	case ErrorRateLimited, ErrorTimeout, ErrorUnavailable:
		return true
	default:
		return false
	}
}

type MiniMaxClient struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

func NewMiniMaxClient(apiKey string) (*MiniMaxClient, error) {
	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newMiniMaxClient(apiKey, Endpoint, client)
}

func newMiniMaxClient(apiKey, endpoint string, httpClient *http.Client) (*MiniMaxClient, error) {
	if strings.TrimSpace(apiKey) != apiKey || apiKey == "" || len(apiKey) > 16<<10 || strings.ContainsAny(apiKey, "\x00\r\n") {
		return nil, &ProviderError{Code: ErrorInvalidRequest}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || httpClient == nil {
		return nil, &ProviderError{Code: ErrorInvalidRequest}
	}
	return &MiniMaxClient{apiKey: apiKey, endpoint: endpoint, httpClient: httpClient}, nil
}

type providerRequest struct {
	Model            string             `json:"model"`
	Prompt           string             `json:"prompt"`
	ResponseFormat   string             `json:"response_format"`
	N                int                `json:"n"`
	AspectRatio      string             `json:"aspect_ratio"`
	PromptOptimizer  bool               `json:"prompt_optimizer"`
	AIGCWatermark    bool               `json:"aigc_watermark"`
	SubjectReference []subjectReference `json:"subject_reference,omitempty"`
}

type subjectReference struct {
	Type      string `json:"type"`
	ImageFile string `json:"image_file"`
}

func (c *MiniMaxClient) Generate(ctx context.Context, request Request) (Result, error) {
	payload, err := providerPayload(request)
	if err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return Result{}, &ProviderError{Code: ErrorRequestCanceled}
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{}, &ProviderError{Code: ErrorTimeout}
		}
		return Result{}, &ProviderError{Code: ErrorUnavailable}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, &ProviderError{Code: httpFailureCode(response.StatusCode)}
	}
	maximum := int64(base64.StdEncoding.EncodedLen(MaxImageBytes) + responseSlack)
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil || int64(len(encoded)) > maximum {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	return decodeProviderResponse(encoded)
}

func providerPayload(request Request) (providerRequest, error) {
	if !utf8.ValidString(request.Prompt) || strings.TrimSpace(request.Prompt) == "" ||
		utf8.RuneCountInString(request.Prompt) > maxPromptRunes || strings.ContainsRune(request.Prompt, '\x00') {
		return providerRequest{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	if request.AspectRatio == "" {
		request.AspectRatio = "1:1"
	}
	if _, ok := aspectRatios[request.AspectRatio]; !ok {
		return providerRequest{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	payload := providerRequest{
		Model: Model, Prompt: request.Prompt, ResponseFormat: "base64", N: 1,
		AspectRatio: request.AspectRatio, PromptOptimizer: false, AIGCWatermark: false,
	}
	switch request.Mode {
	case ModeTextToImage:
		if request.ReferenceDataURL != "" {
			return providerRequest{}, &ProviderError{Code: ErrorInvalidRequest}
		}
	case ModeSubject:
		if err := validateReferenceDataURL(request.ReferenceDataURL); err != nil {
			return providerRequest{}, err
		}
		payload.SubjectReference = []subjectReference{{Type: "character", ImageFile: request.ReferenceDataURL}}
	default:
		return providerRequest{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	return payload, nil
}

type providerResponse struct {
	Data struct {
		ImageBase64 []string `json:"image_base64"`
	} `json:"data"`
	Metadata struct {
		SuccessCount decimalCount `json:"success_count"`
		FailedCount  decimalCount `json:"failed_count"`
	} `json:"metadata"`
	BaseResponse *struct {
		StatusCode *int `json:"status_code"`
	} `json:"base_resp"`
}

type decimalCount int

func (value *decimalCount) UnmarshalJSON(raw []byte) error {
	text := string(raw)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		text = decoded
	}
	if !canonicalCount.MatchString(text) {
		return errors.New("count is not a canonical non-negative integer")
	}
	if text == "0" {
		*value = 0
		return nil
	}
	if text == "1" {
		*value = 1
		return nil
	}
	return errors.New("count exceeds the supported image cardinality")
}

func decodeProviderResponse(encoded []byte) (Result, error) {
	var response providerResponse
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(&response); err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	if response.BaseResponse == nil || response.BaseResponse.StatusCode == nil {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	if *response.BaseResponse.StatusCode != 0 {
		return Result{}, &ProviderError{Code: providerFailureCode(*response.BaseResponse.StatusCode)}
	}
	if response.Metadata.SuccessCount != 1 || response.Metadata.FailedCount != 0 || len(response.Data.ImageBase64) != 1 {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	return decodeImage(response.Data.ImageBase64[0], MaxImageBytes)
}

func decodeImage(encoded string, maximum int) (Result, error) {
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(maximum) {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	content, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(content) == 0 || len(content) > maximum || base64.StdEncoding.EncodeToString(content) != encoded {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
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

func providerFailureCode(status int) string {
	switch status {
	case 1002, 2045:
		return ErrorRateLimited
	case 1004, 2049:
		return ErrorAuthentication
	case 1008:
		return ErrorBalance
	case 1026, 1027:
		return ErrorContentRejected
	case 1001:
		return ErrorTimeout
	case 1024, 1033:
		return ErrorUnavailable
	case 2013:
		return ErrorInvalidRequest
	default:
		return ErrorGenerationFailed
	}
}

func DisplayName(_ string) string {
	// Artifact identity is announced before the provider response reveals its
	// concrete media type. Keep the identity stable across started/completed
	// events; the MIME type remains authoritative for preview and save flows.
	return "generated-image"
}
