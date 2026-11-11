# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Ault Miner Client - A Go-based VRF mining client for the Ault blockchain. Uses Ed25519 VRF (Verifiable Random Function) with micro proof-of-work for fair, decentralized mining. Supports batch transaction submission for gas efficiency and operator/delegation mining pools.

## Build and Development Commands

```bash
# Build
make build                    # Build ./aultmined binary

# Test
make test                     # Run all tests with race detection
make test-short               # Run short tests only
go test -v ./pkg/mining/...   # Run specific package tests

# Lint and Format
make lint                     # Run golangci-lint (auto-installs if missing)
make fmt                      # Format code
make vet                      # Run go vet

# Run
make run                      # Build and start miner
make run-debug                # Start with debug logging

# Docker
make docker-build             # Build Docker image
```

## Architecture

```
cmd/main.go              # CLI entry point (cobra commands: vrfkeygen, set-key, mine)
pkg/
  client/                # Chain interaction layer
    chain.go             # ChainClient - gRPC/RPC connection, signing, broadcasting
    queries.go           # Query methods (epochs, licenses, operator info)
    transactions.go      # Transaction building (MsgSubmitWork, MsgSetOwnerVRFKey)
  mining/
    manager.go           # MinerManager - orchestrates epoch processing and batch submission
    vrf.go               # VRF computation (Ed25519 ECVRF)
    pow.go               # Micro proof-of-work solver
    stats.go             # Mining statistics tracking
internal/
  config/config.go       # Viper-based configuration from env vars
  storage/storage.go     # SQLite persistence (GORM) for submissions and rewards
api/server.go            # Fiber REST API server for monitoring
```

## Key Patterns

**Mining Flow**: MinerManager.Start() -> ProcessEpoch() -> ComputeVRF() for each license -> SolvePoW() if winner -> batch submit all wins in single tx

**Chain Client**: Uses cosmos-sdk tx signing with EIP-712 for EVM compatibility. All chain queries go through gRPC, broadcasts via Tendermint RPC.

**VRF**: Uses github.com/ProtonMail/go-ecvrf for Ed25519 VRF. VRF input = epoch_seed || license_id. Win if VRF output < threshold.

**Batch Submission**: All winning submissions for an epoch are collected and broadcast as a single MsgSubmitWorkBatch transaction to save gas.

## Environment Variables

Required:

- `MINER_OPERATOR_KEY` - secp256k1 private key (hex) for signing transactions
- `MINER_VRF_KEY` - Ed25519 VRF private key (hex) for mining

Optional:

- `CHAIN_GRPC` - default: localhost:9090 (comma-separated list supported for failover)
- `CHAIN_GRPC_TLS` - default: auto (`auto` = try TLS then fall back to plaintext, `true` = force TLS, `false` = force plaintext)
- `CHAIN_RPC` - default: tcp://localhost:26657
- `CHAIN_ID` - default: ault_20904-1
- `MINER_API_PORT` - default: 8080
- `MINER_BATCH_SIZE` - default: 1000 (max submissions per batch, 0 = fallback 1000)
- `MINER_DISABLE_DB` - default: false (set to "true" or "1" to disable SQLite storage)

## Dependencies

The `go.mod` uses Ault-Blockchain forks of:

- cosmos-sdk (v0.53.x with EVM support)
- cometbft (tendermint consensus)
- cosmos/evm (EVM module)

These are pinned via replace directives. When updating, check compatibility with the main Ault chain repo.
