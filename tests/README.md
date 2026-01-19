# Miner Client Setup Scripts

Scripts for setting up and running multiple miner clients via Docker.

## Prerequisites

- Docker installed and running
- `aultd` binary available in PATH
- Running Ault blockchain node

## Quick Start

```bash
cd tests
cp .env.template .env
# Edit .env with your chain configuration
```

## Workflow

### 1. Install Mining Node

Pull the Docker image or build from source.

```bash
./install_mining_node.sh [version]
```

Options:

- `latest` (default)
- Specific version tag (e.g., `v1.0.0`)

#### Using Local Docker Image

To build and use a local Docker image instead of pulling from the registry:

```bash
# Build local image from project root

export GH_PAT=$(gh auth token)

cd ..
docker build --secret id=GH_PAT,env=GH_PAT -t aultmined:local .
cd tests
```

### 2. Mint Licenses

Mint 10,000 licenses to 10 license holders using 4 minters.

```bash
./mint_licenses.sh
```

This script uses hardcoded mnemonics for:

- 4 minters (2,500 licenses each)
- 10 license holders (1,000 licenses each)

### 3. Setup VRF Key

Generate and register VRF keys for 4 operators.

```bash
./setup_vrf_key.sh
```

### 4. Delegate Licenses

Delegate licenses from the 10 license holders to 4 operators.

```bash
./delegate_licenses.sh
```

This script uses hardcoded mnemonics for:

- 10 license holders (each delegates 250 licenses per operator)
- 4 operators (2,500 licenses each = 250 × 10)

This script uses hardcoded operator mnemonics and will:

1. Derive operator private keys from mnemonics
2. Generate VRF key pairs for each operator
3. Register VRF public keys on chain
4. Output VRF private keys to add to `.env`

After running, copy the output `MINER_VRF_KEYS` to your `.env` file.

### 5. Start Miner Client

Start mining with Docker containers.

```bash
./start_miner_client.sh [version]
```

Options:

- `--local`: Use local Docker image (`aultmined:local`) instead of pulling from registry
- `[version]`: Image version tag (default: `latest`)

Examples:

```bash
# Use remote registry image (latest)
./start_miner_client.sh

# Use specific version from registry
./start_miner_client.sh v1.0.0

# Use locally built image
./start_miner_client.sh --local
```

Requires `.env` with:

- `MINER_VRF_KEYS`: comma-separated VRF private keys (from step 3)

Each miner runs on a separate port: miner-1 -> 8080, miner-2 -> 8081, etc.

## Environment Variables

| Variable           | Description                                     | Default                             |
| ------------------ | ----------------------------------------------- | ----------------------------------- |
| `DOCKER_IMAGE`     | Docker image name                               | `ghcr.io/ault-blockchain/aultmined` |
| `CHAIN_GRPC`       | Chain gRPC endpoint                             | `localhost:9090`                    |
| `CHAIN_RPC`        | Chain RPC endpoint                              | `tcp://localhost:26657`             |
| `CHAIN_ID`         | Chain ID                                        | `ault_20904-1`                      |
| `MINER_VRF_KEYS`   | VRF private keys (comma-separated)              | -                                   |
| `MINER_BATCH_SIZE` | Max submissions per batch (<=0 = fallback 1000) | `1000`                              |

## Useful Commands

```bash
# View miner logs
docker logs -f miner-1

# Stop all miners
for i in $(seq 1 4); do docker stop miner-$i; done

# Remove all miners
for i in $(seq 1 4); do docker rm -f miner-$i; done

# Check miner status
docker ps --filter "name=miner-"

# Health check
curl http://localhost:8080/health   # miner-1
curl http://localhost:8081/health   # miner-2
```
