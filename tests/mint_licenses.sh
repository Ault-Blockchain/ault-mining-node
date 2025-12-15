#!/bin/bash
set -e

# Usage: ./mint_licenses.sh
#
# Mints 10,000 licenses to LICENSE_HOLDER using 4 minters (2,500 each)

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
CHAIN_ID="${CHAIN_ID:-ault_4400-1}"
FEEGRANT_MODULE_ADDR="${FEEGRANT_MODULE_ADDR:-ault140h2ttm4yx8tlxeyg3lh8u787lqzrk28gpqkvq}"

# Configuration
TOTAL_LICENSES=10000
LICENSES_PER_MINTER=2500
MINT_BATCH_SIZE=500

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

# Step 2: Batch mint licenses to license holders (rotating through 4 minters)
echo "Step 2: Batch mint $TOTAL_LICENSES licenses to $LICENSE_HOLDER_COUNT license holders"

for batch_start in $(seq 1 $MINT_BATCH_SIZE $TOTAL_LICENSES); do
  batch_end=$((batch_start + MINT_BATCH_SIZE - 1))
  if [ $batch_end -gt $TOTAL_LICENSES ]; then
    batch_end=$TOTAL_LICENSES
  fi
  batch_count=$((batch_end - batch_start + 1))

  # Determine which minter to use (each minter handles 2500 licenses)
  MINTER_IDX=$(( (batch_start - 1) / LICENSES_PER_MINTER ))
  CURRENT_MINTER="minter$MINTER_IDX"

  # Determine which license holder to mint to (each holder gets 1000 licenses)
  HOLDER_IDX=$(( (batch_start - 1) / LICENSES_PER_HOLDER ))
  CURRENT_HOLDER_ADDR="${LICENSE_HOLDER_ADDRS[$HOLDER_IDX]}"

  # Build comma-separated addresses (all same address)
  BATCH_ADDRS=$(printf "${CURRENT_HOLDER_ADDR}%.0s," $(seq 1 $batch_count) | sed 's/,$//')

  # Build comma-separated URIs
  BATCH_URIS=$(for i in $(seq $batch_start $batch_end); do printf "ipfs://delegation-license-${i},"; done | sed 's/,$//')

  TX_RESULT=$(aultd tx license batch-mint "$BATCH_ADDRS" "$BATCH_URIS" "delegation-setup-batch${batch_start}" \
    --from "$CURRENT_MINTER" \
    $KEYRING_FLAGS \
    --node "$CHAIN_RPC" \
    --chain-id "$CHAIN_ID" \
    --gas 50000000 \
    --fees 10000000000000000aault \
    --fee-granter "$FEEGRANT_MODULE_ADDR" \
    --broadcast-mode sync \
    --yes \
    --output json 2>&1)

  TX_HASH=$(echo "$TX_RESULT" | jq -r '.txhash // empty')
  TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

  if [ -z "$TX_HASH" ] || [ "$TX_CODE" != "0" ]; then
    echo "  Failed to batch mint licenses batch $batch_start with $CURRENT_MINTER (code: $TX_CODE)"
    echo "$TX_RESULT"
    exit 1
  fi

  printf "  [%s -> holder%d] Batch %d-%d: TX %s\n" "$CURRENT_MINTER" "$HOLDER_IDX" "$batch_start" "$batch_end" "$TX_HASH"

  # Wait for transaction to be included in a block
  sleep 2
done

echo ""
echo "  Waiting for minting to complete..."
sleep 5

# Query license balances for all holders
echo ""
echo "=== License Minting Complete ==="
echo ""
echo "Summary:"
TOTAL_MINTED=0
for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  HOLDER_ADDR="${LICENSE_HOLDER_ADDRS[$i]}"
  BALANCE=$(aultd q license balance-of "$HOLDER_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null | jq -r '.balance // 0')
  echo "  License holder $i ($HOLDER_ADDR): $BALANCE licenses"
  TOTAL_MINTED=$((TOTAL_MINTED + BALANCE))
done
echo ""
echo "  Total licenses minted: $TOTAL_MINTED"
echo ""
echo "License mapping saved to: $LICENSE_CSV"
echo ""
echo "Next steps:"
echo "  1. Run ./setup_vrf_key.sh to register VRF keys for operators (if not done)"
echo "  2. Run ./delegate_licenses.sh to delegate licenses to operators"
