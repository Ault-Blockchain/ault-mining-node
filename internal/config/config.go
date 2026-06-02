package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Environment variable names
const (
	EnvOperatorKey  = "MINER_OPERATOR_KEY"
	EnvVRFKey       = "MINER_VRF_KEY"
	EnvChainGRPC    = "CHAIN_GRPC"
	EnvChainGRPCTLS = "CHAIN_GRPC_TLS"
	EnvChainRPC     = "CHAIN_RPC"
	EnvChainID      = "CHAIN_ID"
	EnvAPIPort      = "MINER_API_PORT"
	EnvBatchSize    = "MINER_BATCH_SIZE"
	EnvDataDir      = "MINER_DATA_DIR"
)

// Default values
const (
	DefaultGRPCEndpoint = "localhost:9090"
	DefaultRPCEndpoint  = "tcp://localhost:26657"
	DefaultChainID      = "ault_20904-1"
	DefaultAPIPort      = "8080"
	DefaultBatchSize    = 1000   // Max submissions per batch (<=0 = fallback 1000)
	DefaultDataDir      = "data" // Data directory for keys and DB
)

// GRPCTLSMode controls transport security for CHAIN_GRPC endpoints.
type GRPCTLSMode string

const (
	GRPCTLSModeAuto      GRPCTLSMode = "auto"
	GRPCTLSModeForceTLS  GRPCTLSMode = "true"
	GRPCTLSModePlaintext GRPCTLSMode = "false"
	DefaultGRPCTLSMode               = GRPCTLSModeAuto
)

// Config holds all miner configuration values
type Config struct {
	GRPCEndpoint string
	GRPCTLSMode  GRPCTLSMode
	RPCEndpoint  string
	ChainID      string
	OperatorKey  string
	VRFKey       string
	APIPort      string
	BatchSize    int    // Max submissions per batch (<=0 = fallback 1000)
	DataDir      string // Data directory for keys and DB
	AutoMode     bool   // True if no operator/VRF keys set (auto-generate on fly.io)
}

var (
	cfg     *Config
	cfgErr  error
	cfgOnce sync.Once
)

// Load initializes configuration from environment variables.
func Load() error {
	cfgOnce.Do(func() {
		batchSize := DefaultBatchSize
		if v := os.Getenv(EnvBatchSize); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				batchSize = parsed
			}
		}

		operatorKey := os.Getenv(EnvOperatorKey)
		vrfKey := os.Getenv(EnvVRFKey)
		grpcTLSMode, err := parseGRPCTLSMode(os.Getenv(EnvChainGRPCTLS))
		if err != nil {
			cfgErr = err
			return
		}

		cfg = &Config{
			GRPCEndpoint: getEnvOrDefault(EnvChainGRPC, DefaultGRPCEndpoint),
			GRPCTLSMode:  grpcTLSMode,
			RPCEndpoint:  getEnvOrDefault(EnvChainRPC, DefaultRPCEndpoint),
			ChainID:      getEnvOrDefault(EnvChainID, DefaultChainID),
			OperatorKey:  operatorKey,
			VRFKey:       vrfKey,
			APIPort:      getEnvOrDefault(EnvAPIPort, DefaultAPIPort),
			BatchSize:    batchSize,
			DataDir:      getEnvOrDefault(EnvDataDir, DefaultDataDir),
			AutoMode:     operatorKey == "" && vrfKey == "",
		}
	})
	return cfgErr
}

// Get returns the loaded configuration.
// Panics if Load() was not called successfully first.
func Get() *Config {
	if cfg == nil {
		panic("config.Load() must be called before config.Get()")
	}
	return cfg
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseGRPCTLSMode(raw string) (GRPCTLSMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return DefaultGRPCTLSMode, nil
	case string(GRPCTLSModeAuto):
		return GRPCTLSModeAuto, nil
	case string(GRPCTLSModeForceTLS):
		return GRPCTLSModeForceTLS, nil
	case string(GRPCTLSModePlaintext):
		return GRPCTLSModePlaintext, nil
	default:
		return "", fmt.Errorf("invalid %s %q: expected auto, true, or false", EnvChainGRPCTLS, raw)
	}
}
