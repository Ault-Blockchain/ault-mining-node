#!/bin/bash
set -e

# Usage: ./mint_licenses.sh
#
# Mints 10,000 licenses to LICENSE_HOLDER using 4 minters (2,500 each)
#
# Required .env variables:
# - MINTER_KEYS: comma-separated minter private keys (4 minters with mint permission)
#
# Optional .env variables:
# - LICENSE_HOLDER_KEY: license holder's private key (if not set, generates new key)

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
MINT_BATCH_SIZE=100

# Validate required environment variables
if [ -z "$MINTER_KEYS" ]; then
  echo "Error: MINTER_KEYS is not set (comma-separated private keys for 4 minters)"
  exit 1
fi

# Parse minter keys array
IFS=',' read -ra MINTER_KEY_ARR <<< "$MINTER_KEYS"
MINTER_COUNT=${#MINTER_KEY_ARR[@]}

if [ $MINTER_COUNT -ne 4 ]; then
  echo "Error: Expected 4 minter keys, got $MINTER_COUNT"
  exit 1
fi

# Common keyring flags (used throughout script)
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

GENERATED_LICENSE_HOLDER_KEY=""
if [ -z "$LICENSE_HOLDER_KEY" ]; then
  echo "LICENSE_HOLDER_KEY not set, generating new key..."
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete licenseholder $KEYRING_FLAGS -y 2>/dev/null || true
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys add licenseholder $KEYRING_FLAGS --output json >/dev/null 2>&1
  LICENSE_HOLDER_KEY=$(printf '%s\n' "$KEYRING_PASS" | aultd keys unsafe-export-eth-key licenseholder $KEYRING_FLAGS 2>/dev/null)
  GENERATED_LICENSE_HOLDER_KEY="$LICENSE_HOLDER_KEY"
  echo "  Generated LICENSE_HOLDER_KEY: $LICENSE_HOLDER_KEY"
  echo ""
fi

echo "=== License Minting Setup ==="
echo "Total licenses to mint: $TOTAL_LICENSES"
echo "Licenses per minter: $LICENSES_PER_MINTER"
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
  MINTER_KEY="${MINTER_KEY_ARR[$i]}"
  MINTER_NAME="minter$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $MINTER_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys unsafe-import-eth-key $MINTER_NAME "$MINTER_KEY" $KEYRING_FLAGS 2>/dev/null

  MINTER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $MINTER_NAME $KEYRING_FLAGS -a 2>/dev/null)
  if [ -z "$MINTER_ADDR" ]; then
    echo "Error: Failed to import minter$i key"
    exit 1
  fi

  MINTER_ADDRS+=("$MINTER_ADDR")
  echo "    Minter $i: $MINTER_ADDR"
done

# Import license holder key (skip if already generated)
if [ -z "$GENERATED_LICENSE_HOLDER_KEY" ]; then
  echo "  Importing license holder key..."
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete licenseholder $KEYRING_FLAGS -y 2>/dev/null || true
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys unsafe-import-eth-key licenseholder "$LICENSE_HOLDER_KEY" $KEYRING_FLAGS 2>/dev/null
fi
LICENSE_HOLDER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show licenseholder $KEYRING_FLAGS -a 2>/dev/null)
echo "  License holder address: $LICENSE_HOLDER_ADDR"

echo ""

# Step 2: Batch mint licenses to license holder (rotating through 4 minters)
echo "Step 2: Batch mint $TOTAL_LICENSES licenses to license holder (using 4 minters)"

for batch_start in $(seq 1 $MINT_BATCH_SIZE $TOTAL_LICENSES); do
  batch_end=$((batch_start + MINT_BATCH_SIZE - 1))
  if [ $batch_end -gt $TOTAL_LICENSES ]; then
    batch_end=$TOTAL_LICENSES
  fi
  batch_count=$((batch_end - batch_start + 1))

  # Determine which minter to use (each minter handles 2500 licenses)
  MINTER_IDX=$(( (batch_start - 1) / LICENSES_PER_MINTER ))
  CURRENT_MINTER="minter$MINTER_IDX"

  # Build comma-separated addresses (all same address)
  BATCH_ADDRS=$(printf "${LICENSE_HOLDER_ADDR}%.0s," $(seq 1 $batch_count) | sed 's/,$//')

  # Build comma-separated URIs
  BATCH_URIS=$(for i in $(seq $batch_start $batch_end); do printf "ipfs://delegation-license-${i},"; done | sed 's/,$//')

  TX_RESULT=$(aultd tx license batch-mint "$BATCH_ADDRS" "$BATCH_URIS" "delegation-setup-batch${batch_start}" \
    --from "$CURRENT_MINTER" \
    $KEYRING_FLAGS \
    --node "$CHAIN_RPC" \
    --chain-id "$CHAIN_ID" \
    --gas 5000000 \
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

  printf "  [%s] Batch %d-%d: TX %s..." "$CURRENT_MINTER" "$batch_start" "$batch_end" "$TX_HASH"

  # Wait for transaction to be included in a block and check execution result
  while true; do
    sleep 1
    TX_QUERY=$(aultd q tx "$TX_HASH" --node "$CHAIN_RPC" --output json 2>/dev/null)
    if [ -n "$TX_QUERY" ]; then
      TX_EXEC_CODE=$(echo "$TX_QUERY" | jq -r '.code // 0')
      if [ "$TX_EXEC_CODE" = "0" ]; then
        printf " confirmed\n"
      else
        printf " FAILED (code: %s)\n" "$TX_EXEC_CODE"
        echo "  Error: $(echo "$TX_QUERY" | jq -r '.raw_log')"
        exit 1
      fi
      break
    fi
    printf "."
  done
done

echo ""
echo "  Waiting for minting to complete..."
sleep 5

# Query owned licenses
OWNED_LICENSES=$(aultd q license owned-by "$LICENSE_HOLDER_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null)
LICENSE_IDS=($(echo "$OWNED_LICENSES" | jq -r '.license_ids[]'))
echo "  License holder now owns: ${#LICENSE_IDS[@]} licenses"

if [ ${#LICENSE_IDS[@]} -lt $TOTAL_LICENSES ]; then
  echo "Warning: Expected $TOTAL_LICENSES licenses, got ${#LICENSE_IDS[@]}"
fi

echo ""
echo "=== License Minting Complete ==="
echo ""
echo "Summary:"
echo "  License holder: $LICENSE_HOLDER_ADDR"
echo "  Total licenses minted: ${#LICENSE_IDS[@]}"
echo ""

if [ -n "$GENERATED_LICENSE_HOLDER_KEY" ]; then
  echo "Generated License Holder Key (save this!):"
  echo "  LICENSE_HOLDER_KEY=$GENERATED_LICENSE_HOLDER_KEY"
  echo ""
fi

echo "Next step: Run ./delegate_licenses.sh to delegate licenses to operators"
