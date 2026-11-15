package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseUploadMaxBytes(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"50", 50 << 20, false},
		{"0.5", 512 << 10, false},
		{"-1", 0, true},
		{"lots", 0, true},
	}
	for _, c := range cases {
		got, err := parseUploadMaxBytes(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("parseUploadMaxBytes(%q) = %d, %v; want %d, err=%v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func uploadRequest(t *testing.T, name string, size int) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", name)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(bytes.Repeat([]byte("x"), size))
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func withUploadLimit(t *testing.T, max int64) {
	t.Helper()
	origDir, origMax := uploadDir, uploadMaxBytes
	uploadDir, uploadMaxBytes = t.TempDir(), max
	t.Cleanup(func() { uploadDir, uploadMaxBytes = origDir, origMax })
}

func TestUploadNoLimitAcceptsOver50MB(t *testing.T) {
	withUploadLimit(t, 0)
	rr := httptest.NewRecorder()
	handleUpload(rr, uploadRequest(t, "big.zip", 51<<20))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with no limit, got %d: %s", rr.Code, rr.Body.String())
	}
	var refs []FileRef
	if err := json.Unmarshal(rr.Body.Bytes(), &refs); err != nil || len(refs) != 1 || refs[0].Size != 51<<20 {
		t.Fatalf("unexpected refs %v (%v)", refs, err)
	}
}

func TestUploadOverLimitIs413WithReason(t *testing.T) {
	// Small file over a small limit: rejected per file.
	withUploadLimit(t, 1024)
	rr := httptest.NewRecorder()
	handleUpload(rr, uploadRequest(t, "a.bin", 2048))
	if rr.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rr.Body.String(), "limit is 1.0 KB") {
		t.Fatalf("expected 413 naming the limit, got %d: %q", rr.Code, rr.Body.String())
	}

	// Body far over the limit: cut off by the body cap, same answer.
	rr = httptest.NewRecorder()
	handleUpload(rr, uploadRequest(t, "b.bin", 3<<20))
	if rr.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rr.Body.String(), "too large") {
		t.Fatalf("expected 413 from body cap, got %d: %q", rr.Code, rr.Body.String())
	}
}

func TestUploadUnderLimitOK(t *testing.T) {
	withUploadLimit(t, 1<<20)
	rr := httptest.NewRecorder()
	handleUpload(rr, uploadRequest(t, "ok.txt", 1000))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
