#!/bin/bash

# Usage: ./setup_vrf_key.sh [--local]
#
# Generates and registers VRF keys for operators using mnemonics from .env
#
# Options:
#   --local    Use local Docker image (aultmined:local) instead of remote registry

# Trap Ctrl+C to kill all background processes
cleanup() {
  echo ""
  echo "Interrupted! Killing background processes..."
  jobs -p | xargs -r kill 2>/dev/null
  exit 1
}
trap cleanup SIGINT SIGTERM

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Cache directory
CACHE_DIR="$SCRIPT_DIR/.keyring_cache"

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

# Ensure keyring cache exists
if [ ! -d "$CACHE_DIR" ] || [ ! -f "$CACHE_DIR/operator_keys.txt" ]; then
  echo "Keyring cache not found. Running setup_keyring.sh..."
  "$SCRIPT_DIR/setup_keyring.sh"
  if [ $? -ne 0 ]; then
    echo "Error: Failed to setup keyring"
    exit 1
  fi
fi

# Load operator count and keys from cache
OPERATOR_COUNT=$(cat "$CACHE_DIR/operator_count")

declare -a OPERATOR_KEYS_ARRAY=()
while IFS= read -r key; do
  OPERATOR_KEYS_ARRAY+=("$key")
done < "$CACHE_DIR/operator_keys.txt"

echo "=== VRF Key Setup ==="
echo "Chain ID: $CHAIN_ID"
echo "Chain gRPC: $CHAIN_GRPC"
echo "Chain RPC: $CHAIN_RPC"
echo "Operators: $OPERATOR_COUNT"
echo "(Using cached keyring data)"
echo ""

# Create log directory (clean previous logs)
LOG_DIR="$SCRIPT_DIR/vrf_setup_logs"
rm -rf "$LOG_DIR"
mkdir -p "$LOG_DIR"
echo "Logs: $LOG_DIR/"
echo ""

# Export variables for subshells
export CHAIN_GRPC CHAIN_RPC CHAIN_ID IMAGE VERSION LOG_DIR

# Function to setup VRF key for a single operator (runs in background)
# Arguments: op_idx, operator_key
setup_vrf_for_operator() {
  local op_idx=$1
  local operator_key=$2
  local log_file="$LOG_DIR/operator_${op_idx}.log"

  echo "[Operator $op_idx] Starting VRF key setup..." >> "$log_file"

  # Generate VRF key
  local vrf_output=$(docker run --rm "${IMAGE}:${VERSION}" vrfkeygen 2>&1)

  # Parse private key
  local vrf_private_key=$(echo "$vrf_output" | grep -A1 "Private Key" | tail -1 | tr -d '[:space:]')

  if [ -z "$vrf_private_key" ]; then
    echo "[Operator $op_idx] Error: Failed to parse VRF key" >> "$log_file"
    echo "Raw output:" >> "$log_file"
    echo "$vrf_output" >> "$log_file"
    echo "" >> "$log_file"
    echo "STATUS: FAILED" >> "$log_file"
    return 1
  fi

  echo "[Operator $op_idx] VRF Private Key: $vrf_private_key" >> "$log_file"
  # Save VRF key to separate file for later collection
  echo "$vrf_private_key" > "$LOG_DIR/operator_${op_idx}.vrfkey"

  # Register VRF key on chain
  echo "[Operator $op_idx] Registering VRF key on chain..." >> "$log_file"

  local set_key_output=$(docker run --rm --network host \
    -e MINER_OPERATOR_KEY="$operator_key" \
    -e MINER_VRF_KEY="$vrf_private_key" \
    -e CHAIN_GRPC="$CHAIN_GRPC" \
    -e CHAIN_RPC="$CHAIN_RPC" \
    -e CHAIN_ID="$CHAIN_ID" \
    "${IMAGE}:${VERSION}" set-key 2>&1) || true

  echo "" >> "$log_file"
  echo "Set-key output:" >> "$log_file"
  echo "$set_key_output" >> "$log_file"

  # Parse operator address
  local operator_addr=$(echo "$set_key_output" | grep -i "Setting VRF key for owner" | awk '{print $NF}')
  if [ -z "$operator_addr" ]; then
    operator_addr=$(echo "$set_key_output" | grep -i "Operator address:" | awk '{print $NF}')
  fi
  if [ -n "$operator_addr" ]; then
    echo "$operator_addr" > "$LOG_DIR/operator_${op_idx}.addr"
  fi

  # Check for success/failure
  echo "" >> "$log_file"
  if echo "$set_key_output" | grep -qi "error\|failed\|panic"; then
    echo "STATUS: FAILED" >> "$log_file"
  else
    echo "STATUS: SUCCESS" >> "$log_file"
  fi
}

# Configuration for parallel execution
STAGGER_DELAY=2
PARALLEL_BATCH_SIZE=50

echo "Step 1: Generate and register VRF keys (parallel)"
echo "  Running $OPERATOR_COUNT operators in batches of $PARALLEL_BATCH_SIZE (${STAGGER_DELAY}s stagger)..."
echo ""

# Launch operators in parallel batches
declare -a PIDS=()
for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
  operator_key="${OPERATOR_KEYS_ARRAY[$op_idx]}"

  echo "  Starting operator $op_idx"

  setup_vrf_for_operator "$op_idx" "$operator_key" &
  PIDS+=($!)

  # Stagger delay between launches
  if [ $op_idx -lt $((OPERATOR_COUNT-1)) ]; then
    sleep $STAGGER_DELAY
  fi

  # Wait for batch to complete before starting next batch
  if [ $(( (op_idx + 1) % PARALLEL_BATCH_SIZE )) -eq 0 ] && [ $op_idx -lt $((OPERATOR_COUNT-1)) ]; then
    echo "  Waiting for batch to complete..."
    for pid in "${PIDS[@]}"; do
      wait $pid || true
    done
    PIDS=()
    echo "  Batch complete, continuing..."
  fi
done

echo ""
echo "  All $OPERATOR_COUNT operators started. Waiting for completion..."
echo ""

# Wait for all remaining background jobs
for pid in "${PIDS[@]}"; do
  wait $pid || true
done

# Wait for transactions to be included
echo "Waiting for transactions to be included..."
sleep 5

# Collect results and query chain
echo ""
echo "Step 2: Collecting results and verifying..."
echo ""

declare -a VRF_PRIVATE_KEYS=()
declare -a VRF_PUBLIC_KEYS=()
declare -a OPERATOR_ADDRS=()

SUCCESS_COUNT=0
FAIL_COUNT=0

for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  # Read VRF key from file
  if [ -f "$LOG_DIR/operator_${i}.vrfkey" ]; then
    VRF_PRIVATE_KEYS+=("$(cat "$LOG_DIR/operator_${i}.vrfkey")")
  else
    VRF_PRIVATE_KEYS+=("")
  fi

  # Read operator address from file
  if [ -f "$LOG_DIR/operator_${i}.addr" ]; then
    OPERATOR_ADDR=$(cat "$LOG_DIR/operator_${i}.addr")
    OPERATOR_ADDRS+=("$OPERATOR_ADDR")
  else
    OPERATOR_ADDRS+=("")
  fi

  # Check status
  if [ -f "$LOG_DIR/operator_${i}.log" ] && grep -q "STATUS: SUCCESS" "$LOG_DIR/operator_${i}.log"; then
    echo "  Operator $i: SUCCESS"
    SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
  else
    echo "  Operator $i: FAILED (see $LOG_DIR/operator_${i}.log)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
done

# Query chain for registered VRF public keys
echo ""
echo "Querying registered VRF public keys from chain..."
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OPERATOR_ADDR="${OPERATOR_ADDRS[$i]}"

  if [ -z "$OPERATOR_ADDR" ]; then
    VRF_PUBLIC_KEYS+=("")
    continue
  fi

  # Query using aultd if available
  if command -v aultd &> /dev/null; then
    QUERY_RESULT=$(aultd q miner owner-key "$OPERATOR_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null || echo "{}")
    VRF_PUB=$(echo "$QUERY_RESULT" | jq -r '.vrf_key // empty')

    if [ -n "$VRF_PUB" ] && [ "$VRF_PUB" != "null" ]; then
      echo "  Operator $i ($OPERATOR_ADDR): $VRF_PUB"
      VRF_PUBLIC_KEYS+=("$VRF_PUB")
    else
      echo "  Operator $i ($OPERATOR_ADDR): Query failed or not registered"
      VRF_PUBLIC_KEYS+=("")
    fi
  else
    VRF_PUBLIC_KEYS+=("")
  fi
done

# Output summary
echo ""
echo "=== VRF Key Setup Complete ==="
echo ""
echo "Results: $SUCCESS_COUNT succeeded, $FAIL_COUNT failed"

if [ $FAIL_COUNT -gt 0 ]; then
  echo ""
  echo "Failed operators:"
  for i in $(seq 0 $((OPERATOR_COUNT-1))); do
    if [ -f "$LOG_DIR/operator_${i}.log" ] && grep -q "STATUS: FAILED" "$LOG_DIR/operator_${i}.log"; then
      echo "  - Operator $i: see $LOG_DIR/operator_${i}.log"
    fi
  done
fi

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
