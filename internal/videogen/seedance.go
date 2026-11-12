// Package videogen adapts the application-owned Seedance API. The session layer
// owns admission/one-time submission; this client never retries a create.
package videogen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

type Reference struct{ DataURL, Role string }
type Request struct {
	Prompt string
	Images []Reference
}
type Task struct {
	ID       string
	Status   wire.ProviderStatus
	VideoURL string
}
type Result struct{ Bytes []byte }

type Provider interface {
	Create(context.Context, Request) (Task, error)
	Get(context.Context, string) (Task, error)
	Download(context.Context, Task) (Result, error)
}

type ErrorKind string

const (
	Unavailable     ErrorKind = "unavailable"
	Authentication  ErrorKind = "authentication"
	Rejected        ErrorKind = "rejected"
	InvalidResponse ErrorKind = "invalid_response"
	Cancelled       ErrorKind = "cancelled"
	LimitExceeded   ErrorKind = "limit_exceeded"
)

// No URL, prompt, API response or credential is retained in an error.
type Error struct {
	Kind       ErrorKind
	RetryAfter time.Duration
}

func (e *Error) Error() string { return "video provider: " + string(e.Kind) }
func contextFailure(ctx context.Context) *Error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Kind: Cancelled}
	}
	return &Error{Kind: Unavailable}
}

func Kind(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Unavailable
}
func RetryAfter(err error) time.Duration {
	var e *Error
	if errors.As(err, &e) {
		return e.RetryAfter
	}
	return 0
}

func retryAfterHeader(value string, now time.Time) time.Duration {
	// HTTP permits either delta-seconds or an HTTP date. Keep the delay even
	// when it exceeds one observation window; the Host persists the throttle.
	maximum := time.Unix(0, 1<<63-1).Sub(now)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(maximum/time.Second) {
			return maximum
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		if delay := date.Sub(now); delay < maximum {
			return delay
		}
		return maximum
	}
	return 0
}

// Task IDs are supplier-returned identifiers, never model-supplied paths.
func ValidTaskID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

type SeedanceClient struct {
	credential     func(context.Context) (string, error)
	http           *http.Client
	endpoint       string
	allowResultURL func(string) bool
}

func NewSeedanceClient(credential func(context.Context) (string, error)) *SeedanceClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &SeedanceClient{credential: credential, endpoint: wire.Endpoint, allowResultURL: resultURLAllowed, http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func resultURLAllowed(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || len(u.Path) < 2 || len(value) > 16384 {
		return false
	}
	for _, host := range wire.AllowedVideoHosts {
		if u.Host == host {
			return true
		}
	}
	return false
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}
type imageURL struct {
	URL string `json:"url"`
}
type createBody struct {
	Model   string        `json:"model"`
	Content []contentPart `json:"content"`
}

func payload(input Request) ([]byte, error) {
	if strings.TrimSpace(input.Prompt) == "" || len(input.Images) > 10 {
		return nil, &Error{Kind: Rejected}
	}
	parts := []contentPart{{Type: "text", Text: input.Prompt}}
	first, last, refs := 0, 0, 0
	for _, image := range input.Images {
		if !strings.HasPrefix(image.DataURL, "data:image/") {
			return nil, &Error{Kind: Rejected}
		}
		switch image.Role {
		case "first_frame":
			first++
		case "last_frame":
			last++
		case "reference_image":
			refs++
		default:
			return nil, &Error{Kind: Rejected}
		}
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: image.DataURL}, Role: image.Role})
	}
	if first > 1 || last > 1 || last > first || (refs > 0 && (first > 0 || last > 0)) {
		return nil, &Error{Kind: Rejected}
	}
	body, err := json.Marshal(createBody{Model: wire.Model, Content: parts})
	if err != nil || len(body) > 64_000_000 {
		return nil, &Error{Kind: LimitExceeded}
	}
	return body, nil
}

func (c *SeedanceClient) api(ctx context.Context, method, path string, body []byte) (Task, error) {
	if ctx.Err() != nil {
		return Task{}, contextFailure(ctx)
	}
	key, err := c.credential(ctx)
	if ctx.Err() != nil {
		return Task{}, contextFailure(ctx)
	}
	if err != nil || key == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\x00\r\n") {
		return Task{}, &Error{Kind: Authentication}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return Task{}, &Error{Kind: Rejected}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	key = ""
	if err != nil {
		if ctx.Err() != nil {
			return Task{}, contextFailure(ctx)
		}
		return Task{}, &Error{Kind: Unavailable}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := Unavailable
		switch {
		case response.StatusCode == 401 || response.StatusCode == 403:
			kind = Authentication
		case response.StatusCode == 429:
			kind = Unavailable
		case response.StatusCode >= 400 && response.StatusCode < 500:
			kind = Rejected
		}
		retry := retryAfterHeader(response.Header.Get("Retry-After"), time.Now())
		return Task{}, &Error{Kind: kind, RetryAfter: retry}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return Task{}, contextFailure(ctx)
		}
		return Task{}, &Error{Kind: Unavailable}
	}
	if len(data) > 1<<20 {
		return Task{}, &Error{Kind: InvalidResponse}
	}
	var result struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Status  string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	}
	if json.Unmarshal(data, &result) != nil || !ValidTaskID(result.ID) {
		return Task{}, &Error{Kind: InvalidResponse}
	}
	task := Task{ID: result.ID}
	if method == http.MethodPost {
		return task, nil
	}
	if result.Model != wire.Model {
		return Task{}, &Error{Kind: InvalidResponse}
	}
	task.Status = wire.ProviderStatus(result.Status)
	if task.Status.Validate() != nil {
		return Task{}, &Error{Kind: InvalidResponse}
	}
	if task.Status == wire.ProviderStatusSucceeded {
		if !c.allowResultURL(result.Content.VideoURL) {
			return Task{}, &Error{Kind: InvalidResponse}
		}
		task.VideoURL = result.Content.VideoURL
	}
	return task, nil
}

func (c *SeedanceClient) Create(ctx context.Context, input Request) (Task, error) {
	body, err := payload(input)
	if err != nil {
		return Task{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, wire.GenerateTimeoutSeconds*time.Second)
	defer cancel()
	return c.api(ctx, http.MethodPost, "", body)
}
func (c *SeedanceClient) Get(ctx context.Context, id string) (Task, error) {
	if !ValidTaskID(id) {
		return Task{}, &Error{Kind: Rejected}
	}
	ctx, cancel := context.WithTimeout(ctx, wire.QueryTimeoutSeconds*time.Second)
	defer cancel()
	task, err := c.api(ctx, http.MethodGet, "/"+id, nil)
	if err == nil && task.ID != id {
		return Task{}, &Error{Kind: InvalidResponse}
	}
	return task, err
}
func (c *SeedanceClient) Download(ctx context.Context, task Task) (Result, error) {
	if task.Status != wire.ProviderStatusSucceeded || !c.allowResultURL(task.VideoURL) {
		return Result{}, &Error{Kind: InvalidResponse}
	}
	ctx, cancel := context.WithTimeout(ctx, wire.DownloadTimeoutSeconds*time.Second)
	defer cancel()
	// A distinct request deliberately has no Ark bearer, cookie or provider headers.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.VideoURL, nil)
	if err != nil {
		return Result{}, &Error{Kind: InvalidResponse}
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, contextFailure(ctx)
		}
		return Result{}, &Error{Kind: Unavailable}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 410 {
			return Result{}, &Error{Kind: Rejected}
		}
		return Result{}, &Error{Kind: Unavailable, RetryAfter: retryAfterHeader(response.Header.Get("Retry-After"), time.Now())}
	}
	if response.ContentLength > wire.MaxVideoBytes {
		return Result{}, &Error{Kind: LimitExceeded}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, wire.MaxVideoBytes+1))
	if err != nil {
		return Result{}, &Error{Kind: Unavailable}
	}
	if len(content) > wire.MaxVideoBytes {
		return Result{}, &Error{Kind: LimitExceeded}
	}
	if len(content) < 32 || string(content[4:8]) != "ftyp" {
		return Result{}, &Error{Kind: InvalidResponse}
	}
	return Result{Bytes: content}, nil
}
