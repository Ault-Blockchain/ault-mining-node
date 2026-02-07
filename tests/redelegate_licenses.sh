#!/bin/bash

# Usage: ./redelegate_licenses.sh
#
# Redelegates already-delegated licenses to operators with even distribution


# Trap Ctrl+C to kill all background processes
cleanup() {
  echo ""
  echo "Interrupted! Killing background processes..."
  jobs -p | xargs -r kill 2>/dev/null
  exit 1
}
trap cleanup SIGINT SIGTERM

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
KEYRING_DIR="${HOME}/.aultd"

# Cache directory
CACHE_DIR="$SCRIPT_DIR/.keyring_cache"

# Load environment
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
else
  echo "Error: .env file not found"
  exit 1
fi

# Set chain defaults
CHAIN_RPC="${CHAIN_RPC:-tcp://localhost:26657}"
CHAIN_ID="${CHAIN_ID:-ault_20904-1}"

# Configuration
DEFAULT_MAX_GAS_LIMIT=200000
DELEGATION_BATCH_SIZE=1000
# Rate limit: 10 free gas txs per epoch
RATE_LIMIT_PER_EPOCH=10
EPOCH_WAIT_TIME=65

# Ensure keyring cache exists
if [ ! -d "$CACHE_DIR" ] || [ ! -f "$CACHE_DIR/operator_addrs.txt" ] || [ ! -f "$CACHE_DIR/holder_addrs.txt" ]; then
  echo "Keyring cache not found. Running setup_keyring.sh..."
  "$SCRIPT_DIR/setup_keyring.sh"
  if [ $? -ne 0 ]; then
    echo "Error: Failed to setup keyring"
    exit 1
  fi
fi

# Load counts from cache
OPERATOR_COUNT=$(cat "$CACHE_DIR/operator_count")
LICENSE_HOLDER_COUNT=$(cat "$CACHE_DIR/holder_count")

# Load addresses from cache
declare -a OPERATOR_ADDRS=()
while IFS= read -r addr; do
  OPERATOR_ADDRS+=("$addr")
done < "$CACHE_DIR/operator_addrs.txt"

declare -a HOLDER_ADDRS=()
while IFS= read -r addr; do
  HOLDER_ADDRS+=("$addr")
done < "$CACHE_DIR/holder_addrs.txt"

# Build key names array
declare -a HOLDER_KEYS=()
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  HOLDER_KEYS+=("licenseholder$i")
done

echo "=== License Redelegation Setup ==="
echo "License holders: $LICENSE_HOLDER_COUNT"
echo "Operators: $OPERATOR_COUNT"
echo "Batch size: $DELEGATION_BATCH_SIZE"
echo "Chain RPC: $CHAIN_RPC"
echo "Chain ID: $CHAIN_ID"
echo "(Using cached keyring data)"
echo ""

# Step 1: Redelegate licenses from each holder to operators (in parallel)
echo "Step 1: Redelegate licenses from each holder to operators"
echo "  Running $LICENSE_HOLDER_COUNT holders in parallel..."
echo ""

# Create log directory (clean previous logs)
LOG_DIR="$SCRIPT_DIR/redelegation_logs"
rm -rf "$LOG_DIR"
mkdir -p "$LOG_DIR"

# Export variables for subshells
export KEYRING_BACKEND KEYRING_DIR CHAIN_RPC CHAIN_ID
export DEFAULT_MAX_GAS_LIMIT DELEGATION_BATCH_SIZE RATE_LIMIT_PER_EPOCH EPOCH_WAIT_TIME

# Function to redelegate for a single holder (runs in background)
# Queries licenses from node page by page, redelegates each batch immediately
# Arguments: holder_idx, holder_key, holder_addr, operator_addrs_csv, start_op_idx
redelegate_for_holder() {
  local holder_idx=$1
  local holder_key=$2
  local holder_addr=$3
  local operator_addrs_csv=$4
  local start_op_idx=$5
  local log_file="$LOG_DIR/holder_${holder_idx}.log"

  # Parse operator addresses
  IFS='|' read -ra OP_ADDRS <<< "$operator_addrs_csv"
  local op_count=${#OP_ADDRS[@]}

  echo "[Holder $holder_idx] Starting: $holder_key ($holder_addr), start_op_idx=$start_op_idx" >> "$log_file"

  local tx_count=0
  local op_idx=$start_op_idx
  local page_key=""
  local accumulated_csv=""
  local accumulated_count=0
  local pages_per_batch=$((DELEGATION_BATCH_SIZE / 100))

  while true; do
    # Query licenses for this holder (1 page = 100 from chain)
    local query_args="--node $CHAIN_RPC --output json --limit $DELEGATION_BATCH_SIZE"
    if [ -n "$page_key" ]; then
      query_args="$query_args --page-key $page_key"
    fi

    local query_result=""
    for ((retry=1; retry<=5; retry++)); do
      query_result=$(aultd q license licenses-by-owner "$holder_addr" $query_args 2>/dev/null)
      if [ -n "$query_result" ]; then
        break
      fi
      echo "[Holder $holder_idx] Query empty, retry $retry/5..." >> "$log_file"
      sleep 3
    done
    if [ -z "$query_result" ]; then
      echo "[Holder $holder_idx] FAILED: query returned empty after 5 retries" >> "$log_file"
      echo "FAILED" > "$LOG_DIR/holder_${holder_idx}.status"
      return 1
    fi

    # Extract license IDs as comma-separated
    local page_csv=$(echo "$query_result" | jq -r '[.licenses[].id] | join(",")')
    local page_count=$(echo "$query_result" | jq -r '.licenses | length')
    local next_key=$(echo "$query_result" | jq -r '.pagination.next_key // empty')

    if [ "$page_count" = "0" ] || [ -z "$page_csv" ]; then
      # No more pages - delegate whatever is accumulated
      if [ $accumulated_count -gt 0 ]; then
        echo "[Holder $holder_idx] Last batch: redelegating $accumulated_count licenses to Operator $((op_idx))..." >> "$log_file"
        local operator_addr="${OP_ADDRS[$op_idx]}"
        op_idx=$(( (op_idx + 1) % op_count ))
        local batch_gas=$((accumulated_count * DEFAULT_MAX_GAS_LIMIT))

        local tx_result=$(echo "y" | aultd tx miner redelegate-mining "$accumulated_csv" "$operator_addr" \
          --from "$holder_key" \
          --keyring-backend "$KEYRING_BACKEND" \
          --home "$KEYRING_DIR" \
          --node "$CHAIN_RPC" \
          --chain-id "$CHAIN_ID" \
          --gas $batch_gas \
          --fees 0aault \
          --broadcast-mode sync \
          --yes \
          --output json 2>&1) || true

        local tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null)
        local tx_code=$(echo "$tx_result" | jq -r '.code // 0' 2>/dev/null)
        if [ -z "$tx_hash" ] || [ "$tx_code" != "0" ]; then
          echo "[Holder $holder_idx] TX failed (code: $tx_code), continuing..." >> "$log_file"
          echo "[Holder $holder_idx] (Expected if some licenses already delegated to this operator)" >> "$log_file"
          echo "$tx_result" >> "$log_file"
        else
          echo "[Holder $holder_idx] -> TX: $tx_hash" >> "$log_file"
        fi
        tx_count=$((tx_count + 1))
      fi
      echo "[Holder $holder_idx] No more licenses to redelegate" >> "$log_file"
      break
    fi

    # Accumulate this page's IDs
    if [ -z "$accumulated_csv" ]; then
      accumulated_csv="$page_csv"
    else
      accumulated_csv="$accumulated_csv,$page_csv"
    fi
    accumulated_count=$((accumulated_count + page_count))

    echo "[Holder $holder_idx] Fetched page ($page_count), accumulated: $accumulated_count / $DELEGATION_BATCH_SIZE" >> "$log_file"

    # Check if we've accumulated enough for a full batch
    if [ $accumulated_count -ge $DELEGATION_BATCH_SIZE ]; then
      local operator_addr="${OP_ADDRS[$op_idx]}"
      op_idx=$(( (op_idx + 1) % op_count ))

      echo "[Holder $holder_idx] Redelegating $accumulated_count licenses to Operator $((op_idx))..." >> "$log_file"

      local batch_gas=$((accumulated_count * DEFAULT_MAX_GAS_LIMIT))

      local tx_result=$(echo "y" | aultd tx miner redelegate-mining "$accumulated_csv" "$operator_addr" \
        --from "$holder_key" \
        --keyring-backend "$KEYRING_BACKEND" \
        --home "$KEYRING_DIR" \
        --node "$CHAIN_RPC" \
        --chain-id "$CHAIN_ID" \
        --gas $batch_gas \
        --fees 0aault \
        --broadcast-mode sync \
        --yes \
        --output json 2>&1) || true

      local tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null)
      local tx_code=$(echo "$tx_result" | jq -r '.code // 0' 2>/dev/null)

      if [ -z "$tx_hash" ] || [ "$tx_code" != "0" ]; then
        echo "[Holder $holder_idx] TX failed (code: $tx_code), continuing..." >> "$log_file"
          echo "[Holder $holder_idx] (Expected if some licenses already delegated to this operator)" >> "$log_file"
        echo "$tx_result" >> "$log_file"
      else
        echo "[Holder $holder_idx] -> TX: $tx_hash" >> "$log_file"

        sleep 1

        tx_count=$((tx_count + 1))
        if [ $((tx_count % RATE_LIMIT_PER_EPOCH)) -eq 0 ]; then
          echo "[Holder $holder_idx] Rate limit ($tx_count txs). Waiting ${EPOCH_WAIT_TIME}s..." >> "$log_file"
          sleep $EPOCH_WAIT_TIME
          echo "[Holder $holder_idx] Resuming..." >> "$log_file"
        fi
      fi

      # Reset accumulator
      accumulated_csv=""
      accumulated_count=0
    fi

    # Move to next page or flush remaining
    if [ -z "$next_key" ] || [ "$next_key" = "null" ]; then
      # No more pages - delegate remaining accumulated licenses
      if [ $accumulated_count -gt 0 ]; then
        local operator_addr="${OP_ADDRS[$op_idx]}"
        op_idx=$(( (op_idx + 1) % op_count ))

        echo "[Holder $holder_idx] Last batch: redelegating $accumulated_count licenses to Operator $((op_idx))..." >> "$log_file"
        local batch_gas=$((accumulated_count * DEFAULT_MAX_GAS_LIMIT))

        local tx_result=$(echo "y" | aultd tx miner redelegate-mining "$accumulated_csv" "$operator_addr" \
          --from "$holder_key" \
          --keyring-backend "$KEYRING_BACKEND" \
          --home "$KEYRING_DIR" \
          --node "$CHAIN_RPC" \
          --chain-id "$CHAIN_ID" \
          --gas $batch_gas \
          --fees 0aault \
          --broadcast-mode sync \
          --yes \
          --output json 2>&1) || true

        local tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null)
        local tx_code=$(echo "$tx_result" | jq -r '.code // 0' 2>/dev/null)
        if [ -z "$tx_hash" ] || [ "$tx_code" != "0" ]; then
          echo "[Holder $holder_idx] TX failed (code: $tx_code), continuing..." >> "$log_file"
          echo "[Holder $holder_idx] (Expected if some licenses already delegated to this operator)" >> "$log_file"
          echo "$tx_result" >> "$log_file"
        else
          echo "[Holder $holder_idx] -> TX: $tx_hash" >> "$log_file"
        fi
        tx_count=$((tx_count + 1))
      fi
      break
    fi
    page_key="$next_key"
  done

  echo "[Holder $holder_idx] Complete! Total txs: $tx_count" >> "$log_file"
  echo "SUCCESS" > "$LOG_DIR/holder_${holder_idx}.status"
}

# Build operator addresses as pipe-separated string for passing to function
OPERATOR_ADDRS_CSV=$(IFS='|'; echo "${OPERATOR_ADDRS[*]}")

# Step 2: Redelegate licenses from each holder to operators (parallel with staggered start)
# Each holder queries its own licenses from the node and redelegates page by page
STAGGER_DELAY=5
echo "Step 2: Launch parallel redelegation"
echo "  Running $LICENSE_HOLDER_COUNT holders in parallel (${STAGGER_DELAY}s stagger)..."
echo ""

# Calculate operator stride per holder (e.g., 100 operators / 25 holders = 4)
OP_STRIDE=$((OPERATOR_COUNT / LICENSE_HOLDER_COUNT))
if [ $OP_STRIDE -eq 0 ]; then
  OP_STRIDE=1
fi
echo "  Operator stride per holder: $OP_STRIDE"

# Launch all holders in parallel with staggered start
declare -a PIDS=()
for holder_idx in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  holder_key="${HOLDER_KEYS[$holder_idx]}"
  holder_addr="${HOLDER_ADDRS[$holder_idx]}"
  start_op_idx=$((holder_idx * OP_STRIDE % OPERATOR_COUNT))

  echo "  Starting holder $holder_idx ($holder_key) -> op_idx $start_op_idx"

  redelegate_for_holder "$holder_idx" "$holder_key" "$holder_addr" "$OPERATOR_ADDRS_CSV" "$start_op_idx" &
  PIDS+=($!)

  if [ $holder_idx -lt $((LICENSE_HOLDER_COUNT-1)) ]; then
    sleep $STAGGER_DELAY
  fi
done

echo ""
echo "  All $LICENSE_HOLDER_COUNT holders started. Waiting for completion..."
echo "  Logs: $LOG_DIR/"
echo ""

# Wait for all background jobs and check status
FAILED=false
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  wait ${PIDS[$i]} || true
  if [ -f "$LOG_DIR/holder_${i}.status" ]; then
    STATUS=$(cat "$LOG_DIR/holder_${i}.status")
    if [ "$STATUS" = "FAILED" ]; then
      echo "  Holder $i: FAILED (see $LOG_DIR/holder_${i}.log)"
      FAILED=true
    else
      echo "  Holder $i: SUCCESS"
    fi
  else
    echo "  Holder $i: UNKNOWN (no status file)"
    FAILED=true
  fi
done

if [ "$FAILED" = true ]; then
  echo ""
  echo "Some redelegations failed. Check logs in $LOG_DIR/"
  exit 1
fi

echo ""
echo "=== Redelegation Complete ==="
echo ""
echo "Operators:"
for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
  echo "  Operator $((op_idx+1)): ${OPERATOR_ADDRS[$op_idx]}"
done
echo ""
echo "Note: Redelegations will be active from the next epoch"
echo ""
echo "Next step:"
echo "  Run ./start_miner_client.sh to start mining"
