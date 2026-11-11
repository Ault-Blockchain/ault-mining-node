package config

import (
	"sync"
	"testing"
)

func resetConfigForTest() {
	cfg = nil
	cfgErr = nil
	cfgOnce = sync.Once{}
}

func TestParseGRPCTLSMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    GRPCTLSMode
		wantErr bool
	}{
		{name: "default", raw: "", want: GRPCTLSModeAuto},
		{name: "auto", raw: " AUTO ", want: GRPCTLSModeAuto},
		{name: "force TLS", raw: "TRUE", want: GRPCTLSModeForceTLS},
		{name: "plaintext", raw: "false", want: GRPCTLSModePlaintext},
		{name: "invalid", raw: "yes", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGRPCTLSMode(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidGRPCTLSMode(t *testing.T) {
	resetConfigForTest()
	defer resetConfigForTest()
	t.Setenv(EnvChainGRPCTLS, "yes")

	if err := Load(); err == nil {
		t.Fatalf("expected invalid %s error", EnvChainGRPCTLS)
	}
	if cfg != nil {
		t.Fatalf("expected config not to load")
	}
}

func TestLoadStoresTypedGRPCTLSMode(t *testing.T) {
	resetConfigForTest()
	defer resetConfigForTest()
	t.Setenv(EnvChainGRPCTLS, "true")

	if err := Load(); err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if Get().GRPCTLSMode != GRPCTLSModeForceTLS {
		t.Fatalf("got %q, want %q", Get().GRPCTLSMode, GRPCTLSModeForceTLS)
	}
}
