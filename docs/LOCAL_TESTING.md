# Local Testing Guide

This guide covers testing the Ault Miner Client on a local development node.

## Prerequisites

- Go 1.21+
- Make
- jq (for JSON parsing)

## Quick Start (5 Minutes)

### 1. Start Local Node

From the project root:

```bash
./local_node.sh -y
```

This starts a local test node with:

- Chain ID: `ault_20904-1`
- gRPC: `localhost:9090`
- RPC: `localhost:26657`
- Test accounts: `dev0`, `dev1`, `dev2`, `dev3` (each with 10K AULT)

### 2. Build the Miner

```bash
cd miner
make build
```

### 3. Generate VRF Key

```bash
./aultmined vrfkeygen
```

Copy the private key for the next step.

### 4. Set Environment Variables

```bash
# Get dev0's private key
DEV0_KEY=$(aultd keys export dev0 --unsafe --unarmored-hex --keyring-backend test 2>/dev/null)

export MINER_OPERATOR_KEY="$DEV0_KEY"
export MINER_VRF_KEY="<paste-private-key-from-vrfkeygen>"
export CHAIN_GRPC="localhost:9090"
export CHAIN_RPC="tcp://localhost:26657"
```

### 5. Mint Test Licenses

```bash
# Mint 5 licenses to dev0
for i in {1..5}; do
  aultd tx license mint $(aultd keys show dev0 -a --keyring-backend test) \
    --from dev0 --keyring-backend test \
    --gas=200000 --gas-prices=10000000aault -y
  sleep 2
done

# Verify
aultd q license owned-by $(aultd keys show dev0 -a --keyring-backend test)
```

### 6. Register VRF Key

```bash
./aultmined set-key
```

### 7. Start Mining

```bash
./aultmined mine --yes
```

## Test Scenarios

### Scenario 1: Basic Mining

```bash
# Terminal 1: Start node
./local_node.sh -y

# Terminal 2: Start miner
cd miner
./aultmined mine --yes
```

Wait for epoch transitions and check for wins.

### Scenario 2: Delegated Mining

```bash
# Setup operator (dev0)
export MINER_OPERATOR_KEY=$(aultd keys export dev0 --unsafe --unarmored-hex --keyring-backend test 2>/dev/null)
./aultmined vrfkeygen
# Set MINER_VRF_KEY from output

# Register as operator
aultd tx miner register-operator 10 \
  --from dev0 --keyring-backend test \
  --gas=200000 --gas-prices=10000000aault -y

# Register VRF key
./aultmined set-key

# Mint licenses to dev1
for i in {1..3}; do
  aultd tx license mint $(aultd keys show dev1 -a --keyring-backend test) \
    --from dev0 --keyring-backend test \
    --gas=200000 --gas-prices=10000000aault -y
  sleep 2
done

# dev1 delegates to dev0
OPERATOR=$(aultd keys show dev0 -a --keyring-backend test)
aultd tx miner delegate-mining $OPERATOR 1 2 3 \
  --from dev1 --keyring-backend test \
  --gas=200000 --gas-prices=10000000aault -y

# Start mining (will mine delegated licenses)
./aultmined mine --yes
```

### Scenario 3: API Testing

```bash
# Start miner
./aultmined mine --yes &

# Wait for startup
sleep 5

# Health check
curl http://localhost:8080/health | jq

# Status
curl http://localhost:8080/v1/status | jq

# Submissions
curl http://localhost:8080/v1/submissions | jq

# Rewards (after some epochs)
curl "http://localhost:8080/v1/rewards?license_id=1" | jq
```

## Environment Variables

| Variable             | Default                 | Description                                     |
| -------------------- | ----------------------- | ----------------------------------------------- |
| `MINER_OPERATOR_KEY` | (required)              | Account private key (hex)                       |
| `MINER_VRF_KEY`      | (required)              | VRF private key (hex)                           |
| `CHAIN_GRPC`         | `localhost:9090`        | gRPC endpoint                                   |
| `CHAIN_RPC`          | `tcp://localhost:26657` | RPC endpoint                                    |
| `CHAIN_ID`           | `ault_20904-1`          | Chain identifier                                |
| `MINER_API_PORT`     | `8080`                  | API server port                                 |
| `MINER_BATCH_SIZE`   | `1000`                  | Max submissions per batch (<=0 = fallback 1000) |
| `MINER_DISABLE_DB`   | `false`                 | Disable SQLite storage ("true" or "1")          |

## Useful Commands

### Check Epoch Info

```bash
aultd q miner epoch
```

### Check VRF Key Registration

```bash
aultd q miner owner-key $(aultd keys show dev0 -a --keyring-backend test)
```

### Check License Mining Info

```bash
aultd q miner license-miner-info 1
```

### Check Operator Info

```bash
aultd q miner operator-info $(aultd keys show dev0 -a --keyring-backend test)
```

### Check Delegated Licenses

```bash
aultd q miner delegated-licenses $(aultd keys show dev0 -a --keyring-backend test)
```

### Check Rewards

```bash
aultd q miner license-payouts --license-id 1 --from-epoch 0 --to-epoch 100
```

## Troubleshooting

### "MINER_OPERATOR_KEY environment variable is required"

```bash
# Export the key
export MINER_OPERATOR_KEY=$(aultd keys export dev0 --unsafe --unarmored-hex --keyring-backend test 2>/dev/null)
```

### "VRF key not registered on chain"

```bash
./aultmined set-key
```

### "No licenses found"

```bash
# Check owned licenses
aultd q license owned-by $(aultd keys show dev0 -a --keyring-backend test)

# Check delegated licenses
aultd q miner delegated-licenses $(aultd keys show dev0 -a --keyring-backend test)

# Mint if none
aultd tx license mint $(aultd keys show dev0 -a --keyring-backend test) \
  --from dev0 --keyring-backend test \
  --gas=200000 --gas-prices=10000000aault -y
```

### "Failed to connect to gRPC"

```bash
# Check node is running
curl localhost:26657/status

# Restart node if needed
pkill -f aultd
./local_node.sh -y
```

## Running Tests

```bash
cd miner

# Unit tests
make test

# With coverage
go test -cover ./...

# Specific package
go test -v ./pkg/mining/...
```

## Development Tips

1. **Fast epoch testing**: Modify genesis params for shorter epochs
2. **Database inspection**: `sqlite3 miner.db ".tables"` and `sqlite3 miner.db "SELECT * FROM submissions;"`
3. **API debugging**: Use `curl -v` for verbose output
