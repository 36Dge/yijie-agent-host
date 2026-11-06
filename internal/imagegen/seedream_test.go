package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestSeedreamSingleImage(t *testing.T) {
	raw := testPNG(t)
	ref := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	for _, input := range []Request{{Prompt: "一盏台灯", Mode: ModeTextToImage}, {Prompt: "替换背景", Mode: ModeSubject, ReferenceDataURL: ref}} {
		client := NewSeedreamClient(func(context.Context) (string, error) { return "local-test-key", nil })
		const resultURL = "https://ark-content-generation-v2-cn-beijing.tos-cn-beijing.volces.com/example/image.png"
		calls := 0
		client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if r.Method != http.MethodPost || r.URL.String() != SeedreamEndpoint || r.Header.Get("Authorization") != "Bearer local-test-key" {
					t.Fatal("generation request")
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"model": "doubao-seedream-5-0-pro-260628", "prompt": input.Prompt, "size": "1.5K", "watermark": false}
				if input.Mode == ModeSubject {
					want["image"] = ref
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("unexpected image generation options")
				}
				return providerHTTPResponse(200, []byte(`{"model":"doubao-seedream-5-0-pro-260628","data":[{"url":"`+resultURL+`"}]}`)), nil
			}
			if r.Method != http.MethodGet || r.URL.String() != resultURL || r.Header.Get("Authorization") != "" {
				t.Fatal("unexpected image download")
			}
			return providerHTTPResponse(200, raw), nil
		})
		out, err := client.Generate(context.Background(), input)
		if err != nil || calls != 2 || out.MediaType != "image/png" || !bytes.Equal(out.Bytes, raw) {
			t.Fatalf("calls=%d result=%s err=%v", calls, out.MediaType, err)
		}
	}
}

func TestSeedreamRejectedRequestIsNotResubmitted(t *testing.T) {
	client := NewSeedreamClient(func(context.Context) (string, error) { return "local-test-key", nil })
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return providerHTTPResponse(http.StatusTooManyRequests, nil), nil
	})
	_, err := client.Generate(context.Background(), Request{Prompt: "desk lamp", Mode: ModeTextToImage})
	if calls != 1 || ErrorCode(err) != ErrorRateLimited {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestSeedreamCredentialCancelDoesNotSubmit(t *testing.T) {
	client := NewSeedreamClient(func(context.Context) (string, error) {
		return "", &ProviderError{Code: ErrorRequestCanceled}
	})
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("credential cancellation reached provider transport")
		return nil, nil
	})
	_, err := client.Generate(context.Background(), Request{Mode: ModeTextToImage, Prompt: "studio lamp"})
	if ErrorCode(err) != ErrorRequestCanceled {
		t.Fatalf("cancel status = %v", err)
	}
}

func TestSeedreamReferenceBoundaryPrecedesCredential(t *testing.T) {
	client := NewSeedreamClient(func(context.Context) (string, error) {
		t.Fatal("invalid reference entered credential flow")
		return "", nil
	})
	for _, input := range []Request{
		{Mode: ModeSubject, Prompt: "change background"},
		{Mode: ModeSubject, Prompt: "change background", ReferenceDataURL: "previous-turn.png"},
		{Mode: ModeTextToImage, Prompt: "studio lamp", ReferenceDataURL: "data:image/png;base64,AA=="},
	} {
		if _, err := client.Generate(context.Background(), input); ErrorCode(err) != ErrorInvalidRequest {
			t.Fatalf("input accepted: %v", err)
		}
	}
}
