#!/bin/bash

# Usage: ./mint_licenses.sh
#
# Mints 100,000 licenses to 10 license holders using 4 minters in parallel

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
TOTAL_LICENSES=100000
DEFAULT_MAX_GAS_LIMIT=200000
LICENSES_PER_MINTER=$((TOTAL_LICENSES / 4))
MINT_BATCH_SIZE=1000
GAS_LIMIT=$((MINT_BATCH_SIZE * DEFAULT_MAX_GAS_LIMIT))

# Hardcoded minter mnemonics
MINTER_MNEMONICS=(
  "sustain system renew deliver dream multiply rapid dawn mansion prepare measure year firm strong peanut explain seat route slab now purity romance crash onion"
  "train develop license give method circle salon chef hurry record effort cherry trigger clay shield scissors viable will mule slow action super account lunch"
  "ticket curve abandon expire design banana fee switch bomb move soldier sign there parrot skate vacuum cushion concert width guess situate click van suffer"
  "lion leisure enact truth someone oil team option level buzz track device knife moment update poverty few sad ring harsh struggle finger digital shadow"
)
MINTER_COUNT=${#MINTER_MNEMONICS[@]}

# Hardcoded license holder mnemonics (10 holders, 1000 licenses each)
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
LICENSE_HOLDER_COUNT=${#LICENSE_HOLDER_MNEMONICS[@]}
LICENSES_PER_HOLDER=$((TOTAL_LICENSES / LICENSE_HOLDER_COUNT))

# Common keyring flags (used throughout script)
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

echo "=== License Minting Setup ==="
echo "Total licenses to mint: $TOTAL_LICENSES"
echo "License holders: $LICENSE_HOLDER_COUNT"
echo "Licenses per holder: $LICENSES_PER_HOLDER"
echo "Minters: $MINTER_COUNT"
echo "Chain RPC: $CHAIN_RPC"
echo "Chain ID: $CHAIN_ID"
echo ""

# Step 1: Import keys to keyring
echo "Step 1: Import keys to keyring"

# Import minter keys
echo "  Importing minter keys..."
declare -a MINTER_ADDRS=()
for i in $(seq 0 $((MINTER_COUNT-1))); do
  MINTER_NAME="minter$i"
  MNEMONIC="${MINTER_MNEMONICS[$i]}"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $MINTER_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $MINTER_NAME --recover $KEYRING_FLAGS 2>/dev/null

  MINTER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $MINTER_NAME $KEYRING_FLAGS -a 2>/dev/null)
  if [ -z "$MINTER_ADDR" ]; then
    echo "Error: Failed to import minter$i key"
    exit 1
  fi

  MINTER_ADDRS+=("$MINTER_ADDR")
  echo "    Minter $i: $MINTER_ADDR"
done

# Import license holder keys
echo "  Importing license holder keys..."
declare -a LICENSE_HOLDER_ADDRS=()
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  HOLDER_NAME="licenseholder$i"
  MNEMONIC="${LICENSE_HOLDER_MNEMONICS[$i]}"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $HOLDER_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $HOLDER_NAME --recover $KEYRING_FLAGS 2>/dev/null

  HOLDER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $HOLDER_NAME $KEYRING_FLAGS -a 2>/dev/null)
  if [ -z "$HOLDER_ADDR" ]; then
    echo "Error: Failed to import licenseholder$i key"
    exit 1
  fi

  LICENSE_HOLDER_ADDRS+=("$HOLDER_ADDR")
  echo "    License holder $i: $HOLDER_ADDR"
done

echo ""

# Create CSV file with license holder mapping
LICENSE_CSV="$SCRIPT_DIR/license_holders.csv"
echo "holder_index,holder_key,holder_address,first_license,last_license,count" > "$LICENSE_CSV"
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  FIRST_LIC=$((i * LICENSES_PER_HOLDER + 1))
  LAST_LIC=$(((i + 1) * LICENSES_PER_HOLDER))
  echo "$i,licenseholder$i,${LICENSE_HOLDER_ADDRS[$i]},$FIRST_LIC,$LAST_LIC,$LICENSES_PER_HOLDER" >> "$LICENSE_CSV"
done
echo "  Created license mapping: $LICENSE_CSV"
echo ""

# Step 2: Batch mint licenses to license holders (minters run in parallel)
echo "Step 2: Batch mint $TOTAL_LICENSES licenses to $LICENSE_HOLDER_COUNT license holders"
echo "  Running $MINTER_COUNT minters in parallel..."
echo ""

# Create log directory and license data directory
LOG_DIR="$SCRIPT_DIR/mint_logs"
LICENSE_DATA_DIR="$SCRIPT_DIR/license_data"
mkdir -p "$LOG_DIR"
mkdir -p "$LICENSE_DATA_DIR"

# Initialize license ID files for each holder (clear previous data)
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  > "$LICENSE_DATA_DIR/holder_${i}_licenses.txt"
done

# Export variables for subshells
export KEYRING_BACKEND KEYRING_DIR CHAIN_RPC CHAIN_ID
export DEFAULT_MAX_GAS_LIMIT MINT_BATCH_SIZE GAS_LIMIT
export LICENSES_PER_MINTER LICENSES_PER_HOLDER LICENSE_DATA_DIR

# Function to mint for a single minter (runs in background)
mint_for_minter() {
  local minter_idx=$1
  local minter_name=$2
  local start_license=$3
  local end_license=$4
  local holder_addrs_csv=$5
  local log_file="$LOG_DIR/minter_${minter_idx}.log"

  # Parse holder addresses
  IFS='|' read -ra HOLDER_ADDRS <<< "$holder_addrs_csv"

  echo "[Minter $minter_idx] Starting: $minter_name" >> "$log_file"
  echo "[Minter $minter_idx] License range: $start_license - $end_license" >> "$log_file"

  local tx_count=0

  for ((batch_start=start_license; batch_start<=end_license; batch_start+=MINT_BATCH_SIZE)); do
    local batch_end=$((batch_start + MINT_BATCH_SIZE - 1))
    if [ $batch_end -gt $end_license ]; then
      batch_end=$end_license
    fi
    local batch_count=$((batch_end - batch_start + 1))

    # Determine which license holder to mint to
    local holder_idx=$(( (batch_start - 1) / LICENSES_PER_HOLDER ))
    local holder_addr="${HOLDER_ADDRS[$holder_idx]}"

    echo "[Minter $minter_idx] Batch $batch_start-$batch_end ($batch_count) -> holder$holder_idx" >> "$log_file"

    # Build comma-separated addresses (all same address)
    local batch_addrs=$(printf "${holder_addr}%.0s," $(seq 1 $batch_count) | sed 's/,$//')

    # Build comma-separated URIs
    local batch_uris=$(for i in $(seq $batch_start $batch_end); do printf "ipfs://delegation-license-${i},"; done | sed 's/,$//')

    # Calculate gas based on actual batch count
    local batch_gas=$((batch_count * DEFAULT_MAX_GAS_LIMIT))

    local tx_result=$(aultd tx license batch-mint "$batch_addrs" "$batch_uris" "delegation-setup-batch${batch_start}" \
      --from "$minter_name" \
      --keyring-backend "$KEYRING_BACKEND" \
      --home "$KEYRING_DIR" \
      --node "$CHAIN_RPC" \
      --chain-id "$CHAIN_ID" \
      --gas $batch_gas \
      --fees 0aault \
      --broadcast-mode sync \
      --yes \
      --output json 2>&1)

    local tx_hash=$(echo "$tx_result" | jq -r '.txhash // empty' 2>/dev/null)
    local tx_code=$(echo "$tx_result" | jq -r '.code // 0' 2>/dev/null)

    if [ -z "$tx_hash" ] || [ "$tx_code" != "0" ]; then
      echo "[Minter $minter_idx] FAILED (code: $tx_code)" >> "$log_file"
      echo "$tx_result" >> "$log_file"
      echo "FAILED" > "$LOG_DIR/minter_${minter_idx}.status"
      return 1
    fi

    echo "[Minter $minter_idx] -> TX: $tx_hash" >> "$log_file"

    # Wait for TX to be included, then query events to get actual license IDs
    sleep 3
    local tx_events=$(aultd q tx --type=hash "$tx_hash" --node "$CHAIN_RPC" --output json 2>/dev/null)
    local first_id=$(echo "$tx_events" | jq -r '.events[] | select(.type=="batch_mint_license") | .attributes[] | select(.key=="first_id") | .value' 2>/dev/null)
    local last_id=$(echo "$tx_events" | jq -r '.events[] | select(.type=="batch_mint_license") | .attributes[] | select(.key=="last_id") | .value' 2>/dev/null)

    if [ -n "$first_id" ] && [ -n "$last_id" ]; then
      # Append actual license IDs to holder's file (one per line)
      for ((id=first_id; id<=last_id; id++)); do
        echo "$id" >> "$LICENSE_DATA_DIR/holder_${holder_idx}_licenses.txt"
      done
      echo "[Minter $minter_idx] -> Recorded IDs: $first_id - $last_id" >> "$log_file"
    else
      echo "[Minter $minter_idx] -> WARNING: Could not extract license IDs from TX" >> "$log_file"
    fi

    tx_count=$((tx_count + 1))
  done

  echo "[Minter $minter_idx] Complete! Total txs: $tx_count" >> "$log_file"
  echo "SUCCESS" > "$LOG_DIR/minter_${minter_idx}.status"
}

# Build holder addresses as pipe-separated string for passing to function
HOLDER_ADDRS_CSV=$(IFS='|'; echo "${LICENSE_HOLDER_ADDRS[*]}")

# Launch all minters in parallel
declare -a PIDS=()
for minter_idx in $(seq 0 $((MINTER_COUNT-1))); do
  minter_name="minter$minter_idx"
  start_license=$((minter_idx * LICENSES_PER_MINTER + 1))
  end_license=$(((minter_idx + 1) * LICENSES_PER_MINTER))

  echo "  Starting $minter_name (licenses $start_license-$end_license)..."

  mint_for_minter "$minter_idx" "$minter_name" "$start_license" "$end_license" "$HOLDER_ADDRS_CSV" &
  PIDS+=($!)
done

echo ""
echo "  All $MINTER_COUNT minters started. Waiting for completion..."
echo "  Logs: $LOG_DIR/"
echo ""

# Wait for all background jobs and check status
FAILED=false
for i in $(seq 0 $((MINTER_COUNT-1))); do
  wait ${PIDS[$i]} || true
  if [ -f "$LOG_DIR/minter_${i}.status" ]; then
    STATUS=$(cat "$LOG_DIR/minter_${i}.status")
    if [ "$STATUS" = "FAILED" ]; then
      echo "  Minter $i: FAILED (see $LOG_DIR/minter_${i}.log)"
      FAILED=true
    else
      echo "  Minter $i: SUCCESS"
    fi
  else
    echo "  Minter $i: UNKNOWN (no status file)"
    FAILED=true
  fi
done

if [ "$FAILED" = true ]; then
  echo ""
  echo "Some minting failed. Check logs in $LOG_DIR/"
  exit 1
fi

echo ""
echo "  Waiting for final confirmations..."
sleep 5

# Update CSV with actual license data from recorded files
echo ""
echo "=== License Minting Complete ==="
echo ""
echo "Step 3: Update license mapping with actual IDs"
echo "holder_index,holder_key,holder_address,license_count,license_file" > "$LICENSE_CSV"
TOTAL_MINTED=0
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  HOLDER_ADDR="${LICENSE_HOLDER_ADDRS[$i]}"
  LICENSE_FILE="$LICENSE_DATA_DIR/holder_${i}_licenses.txt"

  # Sort the license IDs (they may be out of order due to parallel minting)
  sort -n "$LICENSE_FILE" -o "$LICENSE_FILE"

  LICENSE_COUNT=$(wc -l < "$LICENSE_FILE" | tr -d ' ')
  echo "$i,licenseholder$i,$HOLDER_ADDR,$LICENSE_COUNT,license_data/holder_${i}_licenses.txt" >> "$LICENSE_CSV"
  echo "  Holder $i: $LICENSE_COUNT licenses -> $LICENSE_FILE"
  TOTAL_MINTED=$((TOTAL_MINTED + LICENSE_COUNT))
done
echo ""
echo "Summary:"
echo "  Total licenses minted: $TOTAL_MINTED"
echo "  License mapping: $LICENSE_CSV"
echo "  License IDs: $LICENSE_DATA_DIR/"
echo ""
echo "Next steps:"
echo "  1. Run ./setup_vrf_key.sh to register VRF keys for operators (if not done)"
echo "  2. Run ./delegate_licenses.sh to delegate licenses to operators"
