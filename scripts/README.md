# Miner Client Setup Scripts

Scripts for setting up and running multiple miner clients via Docker.

## Prerequisites

- Docker installed and running
- `aultd` binary available in PATH
- `minerd` binary available in PATH (or use Docker image)
- Running Ault blockchain node (for license minting and VRF key registration)

## Quick Start

### 1. Configure Environment

```bash
cd networks/miner-client
cp .env.template .env
```

Edit `.env` with your configuration:

```env
# Docker Image
DOCKER_IMAGE=ghcr.io/ault-blockchain/minerd

# Chain Configuration
CHAIN_GRPC=localhost:9090
CHAIN_RPC=tcp://localhost:26657
CHAIN_ID=ault_4400-1

# Operator keys (comma-separated, one per miner)
MINER_OPERATOR_KEYS=<key1>,<key2>,<key3>
MINER_VRF_KEYS=  # Leave empty, will be generated

# License setup
LICENSE_PER_OPERATOR=1
MINTER_KEY=<minter_private_key>
```

### 2. Setup Licenses (First Time Only)

This script will:
- Import minter key to keyring
- Generate VRF keys for each operator (if not already set)
- Register VRF keys on-chain
- Mint licenses to each operator

```bash
./setup_licenses.sh
```

After completion, copy the output `MINER_VRF_KEYS` to your `.env` file.

### 3. Start Miner Clients

```bash
./start_miner_client.sh [version]
```

Examples:
```bash
./start_miner_client.sh           # Uses 'latest' tag
./start_miner_client.sh v1.0.0    # Uses specific version
```

This will:
- Pull the Docker image
- Stop any existing miner containers
- Start N miner containers (based on MINER_OPERATOR_KEYS count)
- Map ports: miner-1 -> 8080, miner-2 -> 8081, etc.

## Testing Locally (Without Docker Registry)

### Build Docker Image Locally

```bash
# From repository root
docker build -f miner/Dockerfile -t minerd-test:local .

# Update .env
DOCKER_IMAGE=minerd-test

# Run
./start_miner_client.sh local
```
## Environment Variables Reference

| Variable | Description | Required |
|----------|-------------|----------|
| `DOCKER_IMAGE` | Docker image URL | Yes |
| `CHAIN_GRPC` | gRPC endpoint (default: localhost:9090) | Yes |
| `CHAIN_RPC` | RPC endpoint (default: tcp://localhost:26657) | Yes |
| `CHAIN_ID` | Chain ID (default: ault_4400-1) | Yes |
| `MINER_OPERATOR_KEYS` | Comma-separated operator private keys | Yes |
| `MINER_VRF_KEYS` | Comma-separated VRF private keys | Yes (for start) |
| `LICENSE_PER_OPERATOR` | Licenses to mint per operator | For setup |
| `MINTER_KEY` | Minter's private key | For setup |

## Monitoring

### View Logs
```bash
docker logs -f miner-1
```

### Check Status
```bash
docker ps --filter "name=miner-"
```

### Health Check
```bash
curl http://localhost:8080/health   # miner-1
curl http://localhost:8081/health   # miner-2
```

### Stop All Miners
```bash
for i in $(seq 1 4); do docker stop miner-$i; done
```
