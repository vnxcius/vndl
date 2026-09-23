package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vndl/internal/config"
	"vndl/internal/downloader"
	"vndl/internal/jobs"
	"vndl/internal/middleware"
)

func testServer() *Server {
	cfg := config.Config{JobTTL: time.Minute}
	mgr := jobs.NewManager(cfg)
	rl := middleware.NewRateLimiter(1000, 1000) // effectively unlimited for these tests
	cl := middleware.NewConcurrencyLimiter(6, 15*time.Second)
	probes := downloader.NewProbeCache(0) // disabled by default in tests
	return NewServer(cfg, mgr, rl, cl, probes)
}

// Regression for VNDL-002: request bodies must be size-capped before decoding.
func TestProbeRejectsOversizedBody(t *testing.T) {
	s := testServer()
	body := `{"url":"` + strings.Repeat("a", maxRequestBody*2) + `"}`
	req := httptest.NewRequest("POST", "/api/probe", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.probe(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for oversized body, got %d", w.Code)
	}
}

func TestCreateDownloadRejectsOversizedBody(t *testing.T) {
	s := testServer()
	body := `{"url":"https://youtube.com/watch?v=x","title":"` + strings.Repeat("a", maxRequestBody*2) + `"}`
	req := httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.createDownload(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for oversized body, got %d", w.Code)
	}
}

// Regression for VNDL-005: format_id/container must be validated.
func TestCreateDownloadRejectsMalformedFormatID(t *testing.T) {
	s := testServer()
	body := `{"url":"https://youtube.com/watch?v=x","format_id":"` + strings.Repeat("A/[", 100) + `"}`
	req := httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.createDownload(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for malformed format_id, got %d", w.Code)
	}
}

func TestCreateDownloadRejectsInvalidContainer(t *testing.T) {
	s := testServer()
	body := `{"url":"https://youtube.com/watch?v=x","container":"; rm -rf /"}`
	req := httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.createDownload(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid container, got %d", w.Code)
	}
}

func TestCreateDownloadAcceptsValidRequest(t *testing.T) {
	s := testServer()
	body := `{"url":"https://youtube.com/watch?v=x","format_id":"bestvideo+bestaudio","container":"mp4"}`
	req := httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.createDownload(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201 for valid request, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateDownloadAcceptsEmptyContainer(t *testing.T) {
	s := testServer()
	body := `{"url":"https://youtube.com/watch?v=x"}`
	req := httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.createDownload(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201 when container is omitted, got %d: %s", w.Code, w.Body.String())
	}
}
