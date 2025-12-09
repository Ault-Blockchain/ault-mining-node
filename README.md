# Ault Miner Client

A production-ready mining client for the Ault blockchain. Uses VRF-based mining with proof-of-work to earn AULT rewards.

## Features

- **Batch Submission**: Automatically batches winning submissions to save gas costs
- **Multi-License Support**: Mine with multiple licenses (owned or delegated)
- **VRF-Based Mining**: Secure random number generation for fair mining
- **Auto-Detection**: Automatically detects owned and delegated licenses
- **REST API**: Built-in API for monitoring and status checks
- **SQLite Storage**: Persistent storage for submissions and rewards tracking

## Quick Start

### 1. Build

```bash
cd miner
make build
```

### 2. Generate VRF Key

```bash
./minerd vrfkeygen
```

### 3. Configure

```bash
export MINER_OPERATOR_KEY="<your-account-private-key-hex>"
export MINER_VRF_KEY="<generated-vrf-private-key-hex>"
export MINER_GRPC_ENDPOINT="localhost:9090"
export MINER_RPC_ENDPOINT="http://localhost:26657"
```

### 4. Register VRF Key

```bash
./minerd set-key
```

### 5. Start Mining

```bash
./minerd mine --yes
```

## Commands

| Command | Description |
|---------|-------------|
| `minerd vrfkeygen` | Generate new Ed25519 VRF keypair |
| `minerd set-key` | Register VRF public key on-chain |
| `minerd mine` | Start mining with all detected licenses |
| `minerd mine --yes` | Start mining without confirmation prompt |

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MINER_OPERATOR_KEY` | Yes | - | Account private key (hex, secp256k1) |
| `MINER_VRF_KEY` | Yes | - | VRF private key (hex, Ed25519) |
| `MINER_GRPC_ENDPOINT` | No | `localhost:9090` | gRPC endpoint |
| `MINER_RPC_ENDPOINT` | No | `http://localhost:26657` | RPC endpoint |
| `MINER_API_PORT` | No | `8080` | REST API port |
| `MINER_DB_PATH` | No | `./miner.db` | SQLite database path |

## Documentation

- **[Production Deployment Guide](docs/PRODUCTION.md)** - Systemd, Docker setup
- **[Local Testing Guide](docs/LOCAL_TESTING.md)** - Development and testing scenarios
- **[REST API Reference](docs/API.md)** - Monitoring endpoints and usage

## How Mining Works

1. **Monitor epochs** - New epoch every ~10 minutes
2. **Compute VRF** - Generate verifiable random output for each license
3. **Check threshold** - Win if VRF output < threshold
4. **Solve PoW** - Micro proof-of-work for anti-spam
5. **Batch submit** - Submit all wins in a single transaction

## Operator Mode (Mining Pool)

Register as an operator to mine on behalf of delegated licenses:

```bash
# Register with 10% commission
aultd tx miner register-operator 10 --from <your-key>

# License holders delegate to you
aultd tx miner delegate-mining <operator-addr> <license-ids...> --from <license-owner>

# Start mining - automatically detects delegated licenses
minerd mine --yes
```

## License

Apache 2.0
