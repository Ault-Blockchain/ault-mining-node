package config

import (
	"os"
	"strconv"
	"sync"
)

// Environment variable names
const (
	EnvOperatorKey = "MINER_OPERATOR_KEY"
	EnvVRFKey      = "MINER_VRF_KEY"
	EnvChainGRPC   = "CHAIN_GRPC"
	EnvChainRPC    = "CHAIN_RPC"
	EnvChainID     = "CHAIN_ID"
	EnvAPIPort     = "MINER_API_PORT"
	EnvBatchSize   = "MINER_BATCH_SIZE"
	EnvDisableDB   = "MINER_DISABLE_DB"
	EnvDataDir     = "MINER_DATA_DIR"
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

// Config holds all miner configuration values
type Config struct {
	GRPCEndpoint string
	RPCEndpoint  string
	ChainID      string
	OperatorKey  string
	VRFKey       string
	APIPort      string
	BatchSize    int    // Max submissions per batch (<=0 = fallback 1000)
	DisableDB    bool   // Disable SQLite storage
	DataDir      string // Data directory for keys and DB
	AutoMode     bool   // True if no operator/VRF keys set (auto-generate on fly.io)
}

var (
	cfg     *Config
	cfgOnce sync.Once
)

// Load initializes configuration from environment variables.
func Load() {
	cfgOnce.Do(func() {
		batchSize := DefaultBatchSize
		if v := os.Getenv(EnvBatchSize); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				batchSize = parsed
			}
		}

		disableDB := false
		if v := os.Getenv(EnvDisableDB); v == "true" || v == "1" {
			disableDB = true
		}

		operatorKey := os.Getenv(EnvOperatorKey)
		vrfKey := os.Getenv(EnvVRFKey)

		cfg = &Config{
			GRPCEndpoint: getEnvOrDefault(EnvChainGRPC, DefaultGRPCEndpoint),
			RPCEndpoint:  getEnvOrDefault(EnvChainRPC, DefaultRPCEndpoint),
			ChainID:      getEnvOrDefault(EnvChainID, DefaultChainID),
			OperatorKey:  operatorKey,
			VRFKey:       vrfKey,
			APIPort:      getEnvOrDefault(EnvAPIPort, DefaultAPIPort),
			BatchSize:    batchSize,
			DisableDB:    disableDB,
			DataDir:      getEnvOrDefault(EnvDataDir, DefaultDataDir),
			AutoMode:     operatorKey == "" && vrfKey == "",
		}
	})
}

// Get returns the loaded configuration.
// Panics if Load() was not called first.
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
