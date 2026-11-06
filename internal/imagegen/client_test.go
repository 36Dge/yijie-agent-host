package imagegen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"testing"
)

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
