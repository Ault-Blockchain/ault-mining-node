#!/bin/bash
set -e

# Usage: ./start_miner_client.sh [version]
# Example: ./start_miner_client.sh latest
# Example: ./start_miner_client.sh v1.0.0
#
# Starts 4 miners using hardcoded operator mnemonics
# Port mapping: miner-1 -> 8080, miner-2 -> 8081, etc.
#
# Required .env variables:
# - MINER_VRF_KEYS: comma-separated VRF private keys (from setup_vrf_key.sh)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
KEYRING_DIR="${HOME}/.aultd"
KEYRING_PASS="testpass"

# Load environment
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
else
  echo "Error: .env file not found"
  echo "Please copy .env.template to .env and configure it"
  exit 1
fi

VERSION=${1:-latest}
IMAGE="${DOCKER_IMAGE:-ghcr.io/ault-blockchain/aultmined}"

# Set chain defaults
CHAIN_GRPC="${CHAIN_GRPC:-localhost:9090}"
CHAIN_RPC="${CHAIN_RPC:-tcp://localhost:26657}"
CHAIN_ID="${CHAIN_ID:-ault_4400-1}"

# Hardcoded operator mnemonics
OPERATOR_MNEMONICS=(
  "vicious strike position case imitate march observe seat earth unknown raise weasel left ahead offer museum come rose print stuff fire club coral sweet"
  "almost cart flee render myth foil soap burden vintage decade name focus local clean sheriff easy avoid pottery slab hollow width income potato unveil"
  "secret hair group relief what result obvious glare tobacco maze shock fire egg chair glare fee play bone fan visit motion valve easy session"
  "shock useless season parrot polar thunder lyrics mutual chapter oak goose access category elite bracket mystery symbol reason above bubble forget spell garment fruit"
)
COUNT=${#OPERATOR_MNEMONICS[@]}

# Common keyring flags
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

# Derive operator private keys from mnemonics
echo "Deriving operator keys from mnemonics..."
declare -a OP_KEYS=()
for i in $(seq 0 $((COUNT-1))); do
  MNEMONIC="${OPERATOR_MNEMONICS[$i]}"
  KEY_NAME="operator$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  OPERATOR_KEY=$(printf '%s\n' "$KEYRING_PASS" | aultd keys unsafe-export-eth-key $KEY_NAME $KEYRING_FLAGS 2>/dev/null)
  if [ -z "$OPERATOR_KEY" ]; then
    echo "Error: Failed to derive key for operator$i"
    exit 1
  fi

  OP_KEYS+=("$OPERATOR_KEY")
  echo "  Operator $i key derived"
done
echo ""

# Parse VRF keys from .env
IFS=',' read -ra VRF_KEYS <<< "$MINER_VRF_KEYS"

if [ ${#VRF_KEYS[@]} -eq 0 ]; then
  echo "Error: MINER_VRF_KEYS is empty"
  echo "Please run ./setup_vrf_key.sh first and add the output to .env"
  exit 1
fi

if [ ${#VRF_KEYS[@]} -ne $COUNT ]; then
  echo "Error: VRF keys count mismatch"
  echo "  Operators: $COUNT"
  echo "  MINER_VRF_KEYS count: ${#VRF_KEYS[@]}"
  exit 1
fi

echo "=== Starting Miner Clients ==="
echo "Image: ${IMAGE}:${VERSION}"
echo "Miner count: $COUNT"
echo "Chain: $CHAIN_ID"
echo ""

# Always pull latest image before running
echo "Pulling ${IMAGE}:${VERSION}..."
docker pull "${IMAGE}:${VERSION}"

echo ""

# Stop and remove existing containers
echo "Cleaning up existing containers..."
for i in $(seq 1 $COUNT); do
  docker rm -f "miner-$i" 2>/dev/null || true
done

echo ""

# Base port for API (default: 8079, so miner-1 -> 8080, miner-2 -> 8081, etc.)
BASE_PORT="${MINER_BASE_PORT:-8079}"

# Start miners with individual keys injected
for i in $(seq 1 $COUNT); do
  idx=$((i-1))
  PORT=$((BASE_PORT+i))

  echo "Starting miner-$i (port: $PORT)..."
  docker run -d \
    --name "miner-$i" \
    --restart unless-stopped \
    --network host \
    -e MINER_OPERATOR_KEY="${OP_KEYS[$idx]}" \
    -e MINER_VRF_KEY="${VRF_KEYS[$idx]}" \
    -e CHAIN_GRPC="${CHAIN_GRPC}" \
    -e CHAIN_RPC="${CHAIN_RPC}" \
    -e CHAIN_ID="${CHAIN_ID}" \
    -e MINER_API_PORT="${PORT}" \
    -e MINER_BATCH_SIZE="${MINER_BATCH_SIZE:-100}" \
    -v "miner${i}_data:/app/data" \
    "${IMAGE}:${VERSION}" \
    mine --yes
done

echo ""
echo "=== Started $COUNT miner(s) ==="
echo ""
docker ps --filter "name=miner-" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
echo ""
echo "To view logs: docker logs -f miner-1"
echo "To stop all: for i in \$(seq 1 $COUNT); do docker stop miner-\$i; done"
