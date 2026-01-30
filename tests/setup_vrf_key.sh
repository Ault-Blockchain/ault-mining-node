#!/bin/bash
set -e

# Usage: ./setup_vrf_key.sh [--local]
#
# Generates and registers VRF keys for operators using mnemonics from .env
#
# Options:
#   --local    Use local Docker image (aultmined:local) instead of remote registry

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
KEYRING_DIR="${HOME}/.aultd"
KEYRING_PASS="testpass"

# Parse command line arguments
USE_LOCAL=false
for arg in "$@"; do
  case $arg in
    --local)
      USE_LOCAL=true
      shift
      ;;
  esac
done

# Load environment if exists (for chain config)
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
fi

# Docker image settings
if [ "$USE_LOCAL" = true ]; then
  IMAGE="aultmined"
  VERSION="local"
  echo "Using local Docker image: ${IMAGE}:${VERSION}"
else
  IMAGE="${DOCKER_IMAGE:-ghcr.io/ault-blockchain/aultmined}"
  VERSION="${DOCKER_VERSION:-latest}"
fi

# Check if Docker is available
if ! command -v docker &> /dev/null; then
  echo "Error: Docker not found"
  echo "Please install Docker first: https://docs.docker.com/get-docker/"
  exit 1
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
OPERATOR_COUNT=${#OPERATOR_MNEMONICS[@]}

# Common keyring flags
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

# Derive operator private keys from mnemonics
echo "Deriving operator keys from mnemonics..."
declare -a OPERATOR_KEYS_ARRAY=()
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  MNEMONIC="${OPERATOR_MNEMONICS[$i]}"
  KEY_NAME="operator$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  OPERATOR_KEY=$(printf '%s\n' "$KEYRING_PASS" | aultd keys unsafe-export-eth-key $KEY_NAME $KEYRING_FLAGS 2>/dev/null)
  if [ -z "$OPERATOR_KEY" ]; then
    echo "Error: Failed to derive key for operator$i"
    exit 1
  fi

  OPERATOR_KEYS_ARRAY+=("$OPERATOR_KEY")
  echo "  Operator $i key derived"
done
echo ""

echo "=== VRF Key Setup ==="
echo "Chain ID: $CHAIN_ID"
echo "Chain gRPC: $CHAIN_GRPC"
echo "Chain RPC: $CHAIN_RPC"
echo "Operators: $OPERATOR_COUNT"
echo ""

# Arrays to store generated keys
declare -a VRF_PRIVATE_KEYS=()
declare -a VRF_PUBLIC_KEYS=()

# Arrays to store operator addresses for later query
declare -a OPERATOR_ADDRS=()

# Process each operator - generate and register VRF keys
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OPERATOR_KEY="${OPERATOR_KEYS_ARRAY[$i]}"

  echo "--- Operator $((i+1))/$OPERATOR_COUNT ---"

  # Generate VRF key
  echo "Generating VRF key..."
  VRF_OUTPUT=$(docker run --rm "${IMAGE}:${VERSION}" vrfkeygen)

  # Parse private key
  VRF_PRIVATE_KEY=$(echo "$VRF_OUTPUT" | grep -A1 "Private Key" | tail -1 | tr -d '[:space:]')

  if [ -z "$VRF_PRIVATE_KEY" ]; then
    echo "Error: Failed to parse VRF key"
    echo "Raw output:"
    echo "$VRF_OUTPUT"
    exit 1
  fi

  echo "VRF Private Key: $VRF_PRIVATE_KEY"
  VRF_PRIVATE_KEYS+=("$VRF_PRIVATE_KEY")

  # Register VRF key on chain
  echo "Registering VRF key on chain..."
  SET_KEY_OUTPUT=$(docker run --rm --network host \
    -e MINER_OPERATOR_KEY="$OPERATOR_KEY" \
    -e MINER_VRF_KEY="$VRF_PRIVATE_KEY" \
    -e CHAIN_GRPC="$CHAIN_GRPC" \
    -e CHAIN_RPC="$CHAIN_RPC" \
    -e CHAIN_ID="$CHAIN_ID" \
    "${IMAGE}:${VERSION}" set-key 2>&1) || true

  echo "$SET_KEY_OUTPUT"

  # Parse operator address for later query
  OPERATOR_ADDR=$(echo "$SET_KEY_OUTPUT" | grep -i "Setting VRF key for owner" | awk '{print $NF}')
  if [ -n "$OPERATOR_ADDR" ]; then
    echo "Operator Address: $OPERATOR_ADDR"
    OPERATOR_ADDRS+=("$OPERATOR_ADDR")
  else
    # Try to parse from "Operator address:" line
    OPERATOR_ADDR=$(echo "$SET_KEY_OUTPUT" | grep -i "Operator address:" | awk '{print $NF}')
    if [ -n "$OPERATOR_ADDR" ]; then
      OPERATOR_ADDRS+=("$OPERATOR_ADDR")
    else
      OPERATOR_ADDRS+=("")
    fi
  fi

  echo ""
done

# Wait for transactions to be included
echo "Waiting for transactions to be included..."
sleep 5

# Query chain for registered VRF public keys
echo ""
echo "Querying registered VRF public keys from chain..."
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OPERATOR_ADDR="${OPERATOR_ADDRS[$i]}"

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Operator $((i+1)): Address unknown, skipping query"
    VRF_PUBLIC_KEYS+=("")
    continue
  fi

  # Query using aultd if available
  if command -v aultd &> /dev/null; then
    QUERY_RESULT=$(aultd q miner owner-key "$OPERATOR_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null || echo "{}")
    VRF_PUB=$(echo "$QUERY_RESULT" | jq -r '.vrf_key // empty')

    if [ -n "$VRF_PUB" ] && [ "$VRF_PUB" != "null" ]; then
      echo "Operator $((i+1)) ($OPERATOR_ADDR): $VRF_PUB"
      VRF_PUBLIC_KEYS+=("$VRF_PUB")
    else
      echo "Operator $((i+1)) ($OPERATOR_ADDR): Query failed or not registered"
      VRF_PUBLIC_KEYS+=("")
    fi
  else
    echo "Operator $((i+1)): aultd not found, skipping verification"
    VRF_PUBLIC_KEYS+=("")
  fi
done

# Output summary
echo ""
echo "=== VRF Key Setup Complete ==="
echo ""
echo "Generated $OPERATOR_COUNT VRF key(s)"
echo ""

echo "Add to your .env:"
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  echo "  MINER_VRF_KEY_${i}=${VRF_PRIVATE_KEYS[$i]}"
done
echo ""

echo "Individual keys:"
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  echo "  Operator $((i+1)):"
  echo "    VRF Private: ${VRF_PRIVATE_KEYS[$i]}"
  echo "    VRF Public:  ${VRF_PUBLIC_KEYS[$i]}"
done
echo ""
echo "Next steps:"
echo "  1. Run ./delegate_licenses.sh to delegate licenses to operators"
echo "  2. Run ./start_miner_client.sh to start mining"
