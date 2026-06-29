package httpclient

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestDecompressResponse_Identity(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader("hello")),
	}
	r, err := DecompressResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := io.ReadAll(r)
	if string(data) != "hello" {
		t.Errorf("want 'hello', got %q", string(data))
	}
}

func TestDecompressResponse_Gzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte("gzip body"))
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"gzip"}},
		Body:   io.NopCloser(&buf),
	}
	r, err := DecompressResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := io.ReadAll(r)
	if string(data) != "gzip body" {
		t.Errorf("want 'gzip body', got %q", string(data))
	}
}

func TestDecompressResponse_Deflate(t *testing.T) {
	var buf bytes.Buffer
	fw, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	_, _ = fw.Write([]byte("deflate body"))
	if err := fw.Close(); err != nil {
		t.Fatalf("deflate close: %v", err)
	}

	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"deflate"}},
		Body:   io.NopCloser(&buf),
	}
	r, err := DecompressResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := io.ReadAll(r)
	if string(data) != "deflate body" {
		t.Errorf("want 'deflate body', got %q", string(data))
	}
}

func TestDecompressResponse_Brotli(t *testing.T) {
	var buf bytes.Buffer
	bw := brotli.NewWriter(&buf)
	_, _ = bw.Write([]byte("brotli body"))
	if err := bw.Close(); err != nil {
		t.Fatalf("brotli close: %v", err)
	}

	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"br"}},
		Body:   io.NopCloser(&buf),
	}
	r, err := DecompressResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := io.ReadAll(r)
	if string(data) != "brotli body" {
		t.Errorf("want 'brotli body', got %q", string(data))
	}
}

func TestDecompressResponse_UnsupportedEncoding(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"unsupported-fake"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}
	_, err := DecompressResponse(resp)
	if err == nil {
		t.Fatal("expected error for unsupported encoding")
	}
	if !strings.Contains(err.Error(), "unsupported-fake") {
		t.Errorf("expected error to mention encoding name, got %v", err)
	}
}

func TestDecompressResponse_GzipInvalidBody(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Encoding": []string{"gzip"}},
		Body:   io.NopCloser(strings.NewReader("not-gzip")),
	}
	_, err := DecompressResponse(resp)
	if err == nil {
		t.Fatal("expected error for invalid gzip body")
	}
}
