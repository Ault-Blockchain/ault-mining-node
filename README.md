# Ault Miner Client

A production-ready mining client for the Ault blockchain. Uses VRF-based mining with proof-of-work to earn AULT rewards.

## Features

- **Batch Submission**: Automatically batches winning submissions to save gas costs
- **Multi-License Support**: Mine with multiple licenses (owned or delegated)
- **VRF-Based Mining**: Secure random number generation for fair mining
- **Auto-Detection**: Automatically detects owned and delegated licenses
- **REST API**: Built-in health check endpoint

## Quick Start

### 1. Build

```bash
cd miner
make build
```

### 2. Generate VRF Key

```bash
./aultmined vrfkeygen
```

### 3. Configure

```bash
export MINER_OPERATOR_KEY="<your-account-private-key-hex>"
export MINER_VRF_KEY="<generated-vrf-private-key-hex>"
export CHAIN_GRPC="localhost:9090"
export CHAIN_RPC="tcp://localhost:26657"
```

### 4. Register VRF Key

```bash
./aultmined set-key
```

### 5. Start Mining

```bash
./aultmined mine --yes
```

## Deploy to Fly.io

For hosted deployments (including Fly.io), use the deployment guide for the full, up-to-date instructions.
See **[Fly.io Deployment Guide](docs/FLY_DEPLOYMENT.md)**.

## Commands

| Command                | Description                              |
| ---------------------- | ---------------------------------------- |
| `aultmined keygen`     | Generate new secp256k1 operator wallet   |
| `aultmined vrfkeygen`  | Generate new Ed25519 VRF keypair         |
| `aultmined set-key`    | Register VRF public key on-chain         |
| `aultmined mine`       | Start mining with all detected licenses  |
| `aultmined mine --yes` | Start mining without confirmation prompt |

## Environment Variables

| Variable             | Required | Default                 | Testnet                                     | Description                            |
| -------------------- | -------- | ----------------------- | ------------------------------------------- | -------------------------------------- |
| `MINER_OPERATOR_KEY` | No\*     | -                       | -                                           | Account private key (hex, secp256k1)   |
| `MINER_VRF_KEY`      | No\*     | -                       | -                                           | VRF private key (hex, Ed25519)         |
| `CHAIN_GRPC`         | No       | `localhost:9090`        | `test-grpc.cloud.aultblockchain.xyz:9090`   | gRPC endpoint(s), comma-separated for fallback |
| `CHAIN_RPC`          | No       | `tcp://localhost:26657` | `https://test-rpc.cloud.aultblockchain.xyz` | RPC endpoint(s), comma-separated for fallback  |
| `CHAIN_ID`           | No       | `ault_20904-1`          | `ault_10904-1`                              | Chain identifier                       |
| `MINER_API_PORT`     | No       | `8080`                  | -                                           | REST API port                          |
| `MINER_BATCH_SIZE`   | No       | `1000`                  | -                                           | Max submissions per batch              |
| `MINER_DATA_DIR`     | No       | `data`                  | `/data`                                     | Directory for auto-generated keys      |

\*If both `MINER_OPERATOR_KEY` and `MINER_VRF_KEY` are unset, auto mode is enabled and keys are auto-generated.

## Documentation

- **[Fly.io Deployment Guide](docs/FLY_DEPLOYMENT.md)** - One-click cloud deployment
- **[Production Deployment Guide](docs/PRODUCTION.md)** - Systemd, Docker setup
- **[Windows Mining Guide](docs/WINDOWS.md)** - Run on Windows with pre-built binaries
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
aultmined mine --yes
```

## License

Apache 2.0
