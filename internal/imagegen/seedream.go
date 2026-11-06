package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	SeedreamEndpoint = "https://ark.cn-beijing.volces.com/api/v3/images/generations"
	SeedreamModel    = "doubao-seedream-5-0-pro-260628"
)

// SeedreamClient resolves the credential only for an admitted image request.
// A new GET fetches the signed image URL without the generation credentials.
type SeedreamClient struct {
	credential func(context.Context) (string, error)
	httpClient *http.Client
}

func NewSeedreamClient(credential func(context.Context) (string, error)) *SeedreamClient {
	return &SeedreamClient{
		credential: credential,
		httpClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

type seedreamRequest struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Size      string `json:"size"`
	Watermark bool   `json:"watermark"`
	Image     string `json:"image,omitempty"`
}

func seedreamPayload(input Request) (seedreamRequest, error) {
	if strings.TrimSpace(input.Prompt) == "" || !utf8.ValidString(input.Prompt) ||
		utf8.RuneCountInString(input.Prompt) > maxPromptRunes || strings.ContainsRune(input.Prompt, 0) {
		return seedreamRequest{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	p := seedreamRequest{Model: SeedreamModel, Prompt: input.Prompt, Size: "1.5K", Watermark: false}
	switch input.Mode {
	case ModeSubject:
		if err := validateReferenceDataURL(input.ReferenceDataURL); err != nil {
			return seedreamRequest{}, err
		}
		p.Image = input.ReferenceDataURL
	case ModeTextToImage:
		if input.ReferenceDataURL != "" {
			return seedreamRequest{}, &ProviderError{Code: ErrorInvalidRequest}
		}
	default:
		return seedreamRequest{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	return p, nil
}

// Volcengine documents these Beijing TOS origins for Seedream and its shared
// output bucket. A different origin requires review before downloading.
func seedreamResultURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || len(u.Path) <= 1 {
		return false
	}
	switch u.Host {
	case "ark-content-generation-v2-cn-beijing.tos-cn-beijing.volces.com", "ark-acg-cn-beijing.tos-cn-beijing.volces.com":
		return true
	}
	return false
}

func (c *SeedreamClient) Generate(ctx context.Context, input Request) (Result, error) {
	p, err := seedreamPayload(input)
	if err != nil {
		return Result{}, err
	}
	key, err := c.credential(ctx)
	if err != nil {
		return Result{}, err
	}
	if key == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\x00\r\n") {
		return Result{}, &ProviderError{Code: ErrorAuthentication}
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	body, err := json.Marshal(p)
	if err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, SeedreamEndpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidRequest}
	}
	post.Header.Set("Authorization", "Bearer "+key)
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("Accept", "application/json")
	metadata, err := c.read(ctx, post, 64<<10)
	if err != nil {
		return Result{}, err
	}
	var result struct {
		Model string `json:"model"`
		Data  []struct {
			URL string `json:"url"`
		} `json:"data"`
		Error *struct{} `json:"error"`
	}
	if json.Unmarshal(metadata, &result) != nil || result.Error != nil || result.Model != SeedreamModel ||
		len(result.Data) != 1 || !seedreamResultURL(result.Data[0].URL) {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}

	get, err := http.NewRequestWithContext(ctx, http.MethodGet, result.Data[0].URL, nil)
	if err != nil {
		return Result{}, &ProviderError{Code: ErrorInvalidResponse}
	}
	content, err := c.read(ctx, get, MaxImageBytes)
	if err != nil {
		return Result{}, err
	}
	return inspectImage(content)
}

func (c *SeedreamClient) read(ctx context.Context, request *http.Request, limit int64) ([]byte, error) {
	response, err := c.httpClient.Do(request)
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			code := httpFailureCode(response.StatusCode)
			if request.Method == http.MethodGet {
				code = ErrorUnavailable
			}
			failure := &ProviderError{Code: code, HTTPStatus: response.StatusCode}
			if request.Method == http.MethodPost {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&body) == nil {
					switch body.Error.Code {
					case "SetLimitExceeded", "AccountRateLimitExceeded", "RequestBurstTooFast", "ServerOverloaded",
						"ModelAccountConcurrentRequestsLimitExceeded", "QuotaExceeded", "QuotaExceeded.AgentPlanQuotaExceeded",
						"AccessDenied", "InvalidAuthentication", "AuthenticationError", "ModelNotOpen", "InvalidEndpointOrModel.NotFound":
						failure.VendorCode = body.Error.Code
					}
				}
			}
			return nil, failure
		}
		var data []byte
		data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err == nil {
			if len(data) == 0 || int64(len(data)) > limit {
				return nil, &ProviderError{Code: ErrorInvalidResponse}
			}
			return data, nil
		}
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return nil, &ProviderError{Code: ErrorRequestCanceled}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, &ProviderError{Code: ErrorTimeout}
	default:
		return nil, &ProviderError{Code: ErrorUnavailable}
	}
}
