#!/bin/bash

# Usage: ./delegate_licenses.sh
#
# Delegates licenses from license holders to 4 operators
# Reads license holder info from license_holders.csv (created by mint_licenses.sh)

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
KEYRING_PASS="testpass"

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

# License CSV path (will be created/updated with actual ownership data)
LICENSE_CSV="$SCRIPT_DIR/license_holders.csv"

# License holder count (same as mnemonics array)
LICENSE_HOLDER_COUNT=10

# License data directory (created by mint_licenses.sh)
LICENSE_DATA_DIR="$SCRIPT_DIR/license_data"

# Hardcoded operator mnemonics
OPERATOR_MNEMONICS=(
  "vicious strike position case imitate march observe seat earth unknown raise weasel left ahead offer museum come rose print stuff fire club coral sweet"
  "almost cart flee render myth foil soap burden vintage decade name focus local clean sheriff easy avoid pottery slab hollow width income potato unveil"
  "secret hair group relief what result obvious glare tobacco maze shock fire egg chair glare fee play bone fan visit motion valve easy session"
  "shock useless season parrot polar thunder lyrics mutual chapter oak goose access category elite bracket mystery symbol reason above bubble forget spell garment fruit"
)
OPERATOR_COUNT=${#OPERATOR_MNEMONICS[@]}

# Hardcoded license holder mnemonics (10 holders)
LICENSE_HOLDER_MNEMONICS=(
  "sweet detail acquire aware bless airport method garlic gloom tortoise pumpkin truck lend fancy web luggage noodle parent planet gap seat sad athlete junior"
  "basket curve garden season gossip law bounce health wine blade upset crunch anchor habit card away tribe disorder kitchen peace leopard quick bid pioneer"
  "crash morning exhibit soft half balance day diamond round turtle oppose silver wheel scale pear conduct census jungle clock verify park kidney system material"
  "diagram horse lens height transfer marine pioneer tumble code boil evidence chat squirrel setup cradle enough giraffe addict garment worry adjust obvious cram mirror"
  "sock dream aspect april coil butter deer bargain observe monitor account solve among finish wheel address betray age mushroom champion stomach reject hip holiday"
  "mom drink noble between forest base test patrol tone spoil later stumble reform assist yard among cat tray insect cave code clip blush drastic"
  "avoid prison retire author during viable clutch faith edge sister believe warm nice loud casino hurt identify practice tail useful athlete cheese kitten chronic"
  "cross car cargo basket actress speak demand decrease grant blue wrestle armed wait suffer message puzzle quantum pattern attitude snack acquire gun force problem"
  "toilet corn settle toilet sword citizen credit innocent access armed unknown symbol obey success ten snake matrix clay elevator foot false agent game catch"
  "plug wash fat virus same script front year number happy federal valley swamp kid crater spy laundry only alley kitchen stumble amused ritual fashion"
)

# Common keyring flags (used throughout script)
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

echo "=== License Delegation Setup ==="
echo "License holders: $LICENSE_HOLDER_COUNT"
echo "Operators: $OPERATOR_COUNT"
echo "Batch size: $DELEGATION_BATCH_SIZE"
echo "Chain RPC: $CHAIN_RPC"
echo "Chain ID: $CHAIN_ID"
echo ""

# Step 1: Import keys and build address arrays
echo "Step 1: Import keys"

# Import license holder keys and build arrays
echo "  Importing license holder keys..."
declare -a HOLDER_KEYS=()
declare -a HOLDER_ADDRS=()
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  MNEMONIC="${LICENSE_HOLDER_MNEMONICS[$i]}"
  KEY_NAME="licenseholder$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  HOLDER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)

  if [ -z "$HOLDER_ADDR" ]; then
    echo "Error: Failed to get address for licenseholder$i"
    exit 1
  fi

  HOLDER_KEYS+=("$KEY_NAME")
  HOLDER_ADDRS+=("$HOLDER_ADDR")
  echo "    License holder $i: $HOLDER_ADDR"
done

# Import operator keys
echo "  Importing operator keys..."
declare -a OPERATOR_ADDRS=()
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  MNEMONIC="${OPERATOR_MNEMONICS[$i]}"
  KEY_NAME="operator$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  OPERATOR_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Error: Failed to get address for operator $((i+1))"
    exit 1
  fi

  OPERATOR_ADDRS+=("$OPERATOR_ADDR")
  echo "    Operator $((i+1)): $OPERATOR_ADDR"
done

echo ""

# Step 2: Check VRF keys for operators
echo "Step 2: Check VRF keys for operators"
VRF_MISSING=false
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OPERATOR_ADDR="${OPERATOR_ADDRS[$i]}"
  VRF_KEY=$(aultd q miner owner-key "$OPERATOR_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null | jq -r '.vrf_pubkey // empty')

  if [ -z "$VRF_KEY" ] || [ "$VRF_KEY" = "null" ]; then
    echo "  Operator $((i+1)) ($OPERATOR_ADDR): VRF key NOT registered"
    VRF_MISSING=true
  else
    echo "  Operator $((i+1)) ($OPERATOR_ADDR): VRF key registered"
  fi
done

if [ "$VRF_MISSING" = true ]; then
  echo ""
  echo "Error: Some operators do not have VRF keys registered."
  echo "Please run setup_vrf_key.sh first:"
  echo "  bash ./tests/setup_vrf_key.sh"
  exit 1
fi

echo ""

# Step 3: Delegate licenses from each holder to operators (in parallel)
echo "Step 3: Delegate licenses from each holder to operators"
echo "  Running $LICENSE_HOLDER_COUNT holders in parallel..."
echo ""

# Create log directory
LOG_DIR="$SCRIPT_DIR/delegation_logs"
mkdir -p "$LOG_DIR"

# Export variables for subshells
export KEYRING_BACKEND KEYRING_DIR CHAIN_RPC CHAIN_ID
export DEFAULT_MAX_GAS_LIMIT DELEGATION_BATCH_SIZE RATE_LIMIT_PER_EPOCH EPOCH_WAIT_TIME

# Function to delegate for a single holder (runs in background)
# Arguments: holder_idx, holder_key, license_ids_csv, operator_addrs_csv
delegate_for_holder() {
  local holder_idx=$1
  local holder_key=$2
  local license_ids_csv=$3
  local operator_addrs_csv=$4
  local log_file="$LOG_DIR/holder_${holder_idx}.log"

  # Parse operator addresses
  IFS='|' read -ra OP_ADDRS <<< "$operator_addrs_csv"
  local op_count=${#OP_ADDRS[@]}

  # Parse license IDs into array
  IFS=',' read -ra LICENSE_IDS <<< "$license_ids_csv"
  local total_count=${#LICENSE_IDS[@]}

  echo "[Holder $holder_idx] Starting: $holder_key" >> "$log_file"
  echo "[Holder $holder_idx] Total licenses: $total_count" >> "$log_file"

  # Track tx count for this signer's rate limit
  local tx_count=0

  # Calculate licenses per operator for this holder
  local licenses_per_op=$((total_count / op_count))

  local license_offset=0
  for op_idx in $(seq 0 $((op_count-1))); do
    local operator_addr="${OP_ADDRS[$op_idx]}"

    # Calculate license range indices for this operator
    local start_idx=$license_offset
    local end_idx=$((license_offset + licenses_per_op - 1))
    if [ $end_idx -ge $total_count ]; then
      end_idx=$((total_count - 1))
    fi

    # Delegate in batches
    for ((batch_start_idx=start_idx; batch_start_idx<=end_idx; batch_start_idx+=DELEGATION_BATCH_SIZE)); do
      local batch_end_idx=$((batch_start_idx + DELEGATION_BATCH_SIZE - 1))
      if [ $batch_end_idx -gt $end_idx ]; then
        batch_end_idx=$end_idx
      fi

      local batch_count=$((batch_end_idx - batch_start_idx + 1))

      # Build comma-separated batch of license IDs
      local batch_csv=""
      for ((i=batch_start_idx; i<=batch_end_idx; i++)); do
        if [ -n "$batch_csv" ]; then
          batch_csv="${batch_csv},"
        fi
        batch_csv="${batch_csv}${LICENSE_IDS[$i]}"
      done

      echo "[Holder $holder_idx] Delegating batch ($batch_count IDs) to Operator $((op_idx+1))..." >> "$log_file"

      # Calculate gas based on actual batch count
      local batch_gas=$((batch_count * DEFAULT_MAX_GAS_LIMIT))

      local tx_result=$(echo "y" | aultd tx miner delegate-mining "$batch_csv" "$operator_addr" \
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
        echo "[Holder $holder_idx] FAILED (code: $tx_code)" >> "$log_file"
        echo "$tx_result" >> "$log_file"
        echo "FAILED" > "$LOG_DIR/holder_${holder_idx}.status"
        return 1
      fi

      echo "[Holder $holder_idx] -> TX: $tx_hash" >> "$log_file"

      # Wait for TX to be confirmed before sending next TX
      for ((wait_i=1; wait_i<=30; wait_i++)); do
        local tx_query=$(aultd q tx --type=hash "$tx_hash" --node "$CHAIN_RPC" --output json 2>/dev/null)
        local tx_height=$(echo "$tx_query" | jq -r '.height // "0"' 2>/dev/null)
        if [ "$tx_height" != "0" ] && [ "$tx_height" != "null" ] && [ -n "$tx_height" ]; then
          echo "[Holder $holder_idx] -> Confirmed at height $tx_height" >> "$log_file"
          break
        fi
        sleep 1
      done

      # Increment tx count and check rate limit (per signer)
      tx_count=$((tx_count + 1))
      if [ $((tx_count % RATE_LIMIT_PER_EPOCH)) -eq 0 ]; then
        echo "[Holder $holder_idx] Rate limit ($tx_count txs). Waiting ${EPOCH_WAIT_TIME}s..." >> "$log_file"
        sleep $EPOCH_WAIT_TIME
        echo "[Holder $holder_idx] Resuming..." >> "$log_file"
      fi
    done

    license_offset=$((license_offset + licenses_per_op))
  done

  echo "[Holder $holder_idx] Complete! Total txs: $tx_count" >> "$log_file"
  echo "SUCCESS" > "$LOG_DIR/holder_${holder_idx}.status"
}

# Build operator addresses as pipe-separated string for passing to function
OPERATOR_ADDRS_CSV=$(IFS='|'; echo "${OPERATOR_ADDRS[*]}")

# Step 3.5: Load license IDs from files (created by mint_licenses.sh)
echo "Step 3.5: Load license IDs from files"
if [ ! -d "$LICENSE_DATA_DIR" ]; then
  echo "Error: License data directory not found: $LICENSE_DATA_DIR"
  echo "Please run mint_licenses.sh first"
  exit 1
fi

declare -a HOLDER_LICENSE_IDS=()
for holder_idx in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  license_file="$LICENSE_DATA_DIR/holder_${holder_idx}_licenses.txt"

  if [ ! -f "$license_file" ]; then
    echo "Error: License file not found: $license_file"
    exit 1
  fi

  # Read license IDs from file (one per line) and convert to comma-separated
  license_ids=$(cat "$license_file" | tr '\n' ',' | sed 's/,$//')
  license_count=$(wc -l < "$license_file" | tr -d ' ')

  echo "  Holder $holder_idx: $license_count licenses from $license_file"
  HOLDER_LICENSE_IDS+=("$license_ids")
done
echo ""

# Step 4: Delegate licenses from each holder to operators (in parallel)
echo "Step 4: Delegate licenses from each holder to operators"
echo "  Running $LICENSE_HOLDER_COUNT holders in parallel..."
echo ""

# Launch all holders in parallel
declare -a PIDS=()
for holder_idx in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  holder_key="${HOLDER_KEYS[$holder_idx]}"
  license_ids="${HOLDER_LICENSE_IDS[$holder_idx]}"

  echo "  Starting holder $holder_idx ($holder_key)..."

  delegate_for_holder "$holder_idx" "$holder_key" "$license_ids" "$OPERATOR_ADDRS_CSV" &
  PIDS+=($!)
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
  echo "Some delegations failed. Check logs in $LOG_DIR/"
  exit 1
fi

echo ""
echo "=== Delegation Complete ==="
echo ""
echo "Operators:"
for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
  echo "  Operator $((op_idx+1)): ${OPERATOR_ADDRS[$op_idx]}"
done
echo ""
echo "Note: Delegations will be active from the next epoch"
echo ""
echo "Next step:"
echo "  Run ./start_miner_client.sh to start mining"
