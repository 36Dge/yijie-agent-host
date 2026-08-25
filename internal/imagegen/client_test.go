package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMiniMaxClientGeneratesTextAndSubjectReferenceImages(t *testing.T) {
	imageBytes := testPNG(t)
	reference := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)
	requests := make([]map[string]any, 0, 2)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer secret-key" ||
			request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected provider request metadata")
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		requests = append(requests, payload)
		response, err := json.Marshal(map[string]any{
			"data":      map[string]any{"image_base64": []string{base64.StdEncoding.EncodeToString(imageBytes)}},
			"metadata":  map[string]any{"success_count": "1", "failed_count": 0},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
		})
		if err != nil {
			return nil, err
		}
		return providerHTTPResponse(http.StatusOK, response), nil
	})
	client, err := newMiniMaxClient("secret-key", Endpoint, &http.Client{Timeout: time.Second, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Generate(context.Background(), Request{Prompt: "draw a clean poster", Mode: ModeTextToImage, AspectRatio: "16:9"})
	if err != nil || result.MediaType != "image/png" || !bytes.Equal(result.Bytes, imageBytes) || result.Width != 2 || result.Height != 1 {
		t.Fatalf("unexpected text-to-image result: %+v err=%v", result, err)
	}
	t2i := requests[0]
	if t2i["model"] != Model || t2i["response_format"] != "base64" || t2i["n"] != float64(1) || t2i["aspect_ratio"] != "16:9" {
		t.Fatalf("provider invariants changed: %#v", t2i)
	}
	if t2i["prompt_optimizer"] != false || t2i["aigc_watermark"] != false {
		t.Fatalf("provider safety defaults changed: %#v", t2i)
	}
	if _, exists := t2i["subject_reference"]; exists {
		t.Fatalf("text-to-image unexpectedly sent a reference: %#v", t2i)
	}

	_, err = client.Generate(context.Background(), Request{Prompt: "keep the person and change the background", Mode: ModeSubject, ReferenceDataURL: reference})
	if err != nil {
		t.Fatal(err)
	}
	i2i := requests[1]
	references, ok := i2i["subject_reference"].([]any)
	if !ok || len(references) != 1 {
		t.Fatalf("subject reference missing: %#v", i2i)
	}
	referencePayload, ok := references[0].(map[string]any)
	if !ok || referencePayload["type"] != "character" || referencePayload["image_file"] != reference {
		t.Fatalf("subject reference changed: %#v", references[0])
	}
}

func TestMiniMaxClientRejectsProviderAndImageFailuresContentFree(t *testing.T) {
	imageBytes := testPNG(t)
	tests := []struct {
		name     string
		status   int
		response any
		wantCode string
	}{
		{name: "http auth", status: http.StatusUnauthorized, response: map[string]any{"secret": "raw"}, wantCode: ErrorAuthentication},
		{name: "provider balance", status: http.StatusOK, response: map[string]any{"base_resp": map[string]any{"status_code": 1008, "status_msg": "raw provider secret"}}, wantCode: ErrorBalance},
		{name: "missing base response", status: http.StatusOK, response: map[string]any{"data": map[string]any{"image_base64": []string{base64.StdEncoding.EncodeToString(imageBytes)}}, "metadata": map[string]any{"success_count": 1, "failed_count": 0}}, wantCode: ErrorInvalidResponse},
		{name: "missing provider status", status: http.StatusOK, response: map[string]any{"data": map[string]any{"image_base64": []string{base64.StdEncoding.EncodeToString(imageBytes)}}, "metadata": map[string]any{"success_count": 1, "failed_count": 0}, "base_resp": map[string]any{"status_msg": "success"}}, wantCode: ErrorInvalidResponse},
		{name: "conflicting count", status: http.StatusOK, response: map[string]any{"data": map[string]any{"image_base64": []string{base64.StdEncoding.EncodeToString(imageBytes)}}, "metadata": map[string]any{"success_count": 1, "failed_count": 1}, "base_resp": map[string]any{"status_code": 0}}, wantCode: ErrorInvalidResponse},
		{name: "malformed base64", status: http.StatusOK, response: map[string]any{"data": map[string]any{"image_base64": []string{"%%%raw-provider-canary%%%"}}, "metadata": map[string]any{"success_count": 1, "failed_count": 0}, "base_resp": map[string]any{"status_code": 0}}, wantCode: ErrorInvalidResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, marshalErr := json.Marshal(test.response)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			client, err := newMiniMaxClient("secret-key", Endpoint, &http.Client{
				Timeout: time.Second,
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return providerHTTPResponse(test.status, response), nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Generate(context.Background(), Request{Prompt: "safe synthetic prompt", Mode: ModeTextToImage})
			if ErrorCode(err) != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", ErrorCode(err), test.wantCode, err)
			}
			if strings.Contains(err.Error(), "raw") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "canary") {
				t.Fatalf("provider body escaped stable error: %v", err)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func providerHTTPResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestProviderPayloadRejectsOpenParametersAndInvalidReferences(t *testing.T) {
	validPNG := testPNG(t)
	validReference := "data:image/png;base64," + base64.StdEncoding.EncodeToString(validPNG)
	for _, request := range []Request{
		{Prompt: " ", Mode: ModeTextToImage},
		{Prompt: strings.Repeat("界", maxPromptRunes+1), Mode: ModeTextToImage},
		{Prompt: "x", Mode: "image-01-live"},
		{Prompt: "x", Mode: ModeTextToImage, AspectRatio: "5:4"},
		{Prompt: "x", Mode: ModeTextToImage, ReferenceDataURL: validReference},
		{Prompt: "x", Mode: ModeSubject},
		{Prompt: "x", Mode: ModeSubject, ReferenceDataURL: "https://example.invalid/reference.png"},
		{Prompt: "x", Mode: ModeSubject, ReferenceDataURL: "data:image/png;base64,%%%"},
	} {
		if _, err := providerPayload(request); ErrorCode(err) != ErrorInvalidRequest {
			t.Fatalf("invalid request accepted or misclassified: %#v err=%v", request, err)
		}
	}
}

func TestDisplayNameIsStableBeforeProviderMediaTypeIsKnown(t *testing.T) {
	if DisplayName("image/png") != "generated-image" ||
		DisplayName("image/jpeg") != "generated-image" {
		t.Fatal("provider image artifact identity changes with the response media type")
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	value.Set(0, 0, color.NRGBA{R: 0x22, G: 0x88, B: 0x66, A: 0xff})
	value.Set(1, 0, color.NRGBA{R: 0xee, G: 0xdd, B: 0xbb, A: 0xff})
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
