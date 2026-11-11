package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ault-Blockchain/ault-miner-node/internal/version"
)

func TestVersionEndpoint(t *testing.T) {
	s := New()
	s.mountRoutes()

	resp, err := s.app.Test(httptest.NewRequest(http.MethodGet, "/version", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var got version.Info
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", body, err)
	}
	if got.Version != version.Version {
		t.Errorf("version = %q, want %q", got.Version, version.Version)
	}
	if got.GoVersion == "" || got.Platform == "" {
		t.Errorf("missing go version/platform: %+v", got)
	}
}
