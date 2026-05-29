#!/bin/bash
set -e

# Usage: ./start_miner_client.sh [--local] [version]
# Example: ./start_miner_client.sh latest
# Example: ./start_miner_client.sh --local
# Example: ./start_miner_client.sh v1.0.0
#
# Starts miners using operator mnemonics from .env (OPERATOR_MNEMONIC_0, OPERATOR_MNEMONIC_1, ...)
# Port mapping: miner-1 -> 8080, miner-2 -> 8081, etc.
#
# Options:
#   --local    Use local Docker image (aultmined:local) instead of remote registry
#
# Required .env variables:
# - OPERATOR_MNEMONIC_0..N: operator mnemonics (indexed from 0)
# - MINER_VRF_KEY_0..N: VRF private keys (indexed from 0, from setup_vrf_key.sh)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
KEYRING_DIR="${HOME}/.aultd"
KEYRING_PASS="testpass"

# Parse command line arguments
USE_LOCAL=false
VERSION=""
for arg in "$@"; do
  case $arg in
    --local)
      USE_LOCAL=true
      ;;
    *)
      VERSION="$arg"
      ;;
  esac
done
VERSION=${VERSION:-latest}

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

# Docker image settings
if [ "$USE_LOCAL" = true ]; then
  IMAGE="aultmined"
  VERSION="local"
  echo "Using local Docker image: ${IMAGE}:${VERSION}"
else
  IMAGE="${DOCKER_IMAGE:-ghcr.io/ault-blockchain/aultmined}"
fi

# Set chain defaults
CHAIN_GRPC="${CHAIN_GRPC:-localhost:9090}"
CHAIN_RPC="${CHAIN_RPC:-tcp://localhost:26657}"
CHAIN_ID="${CHAIN_ID:-ault_20904-1}"

# Load operator mnemonics from env (OPERATOR_MNEMONIC_0, OPERATOR_MNEMONIC_1, ...)
declare -a OPERATOR_MNEMONICS=()
i=0
while true; do
  eval val="\$OPERATOR_MNEMONIC_${i}"
  if [ -z "$val" ]; then break; fi
  OPERATOR_MNEMONICS+=("$val")
  i=$((i+1))
done

if [ ${#OPERATOR_MNEMONICS[@]} -eq 0 ]; then
  echo "Error: No OPERATOR_MNEMONIC_* variables found in .env"
  exit 1
fi
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

# Load VRF keys from env (MINER_VRF_KEY_0, MINER_VRF_KEY_1, ...)
declare -a VRF_KEYS=()
i=0
while true; do
  eval val="\$MINER_VRF_KEY_${i}"
  if [ -z "$val" ]; then break; fi
  VRF_KEYS+=("$val")
  i=$((i+1))
done

if [ ${#VRF_KEYS[@]} -eq 0 ]; then
  echo "Error: No MINER_VRF_KEY_* variables found in .env"
  echo "Please run ./setup_vrf_key.sh first and add the output to .env"
  exit 1
fi

if [ ${#VRF_KEYS[@]} -ne $COUNT ]; then
  echo "Error: VRF keys count mismatch"
  echo "  Operators: $COUNT"
  echo "  MINER_VRF_KEY_* count: ${#VRF_KEYS[@]}"
  exit 1
fi

echo "=== Starting Miner Clients ==="
echo "Image: ${IMAGE}:${VERSION}"
echo "Miner count: $COUNT"
echo "Chain: $CHAIN_ID"
echo ""

# Pull image if not using local
if [ "$USE_LOCAL" != "true" ]; then
  echo "Pulling ${IMAGE}:${VERSION}..."
  docker pull "${IMAGE}:${VERSION}"
fi

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
    -e MINER_BATCH_SIZE="${MINER_BATCH_SIZE:-1000}" \
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
