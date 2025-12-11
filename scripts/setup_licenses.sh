#!/bin/bash
set -e

# Usage: ./setup_licenses.sh
#
# Reads configuration from .env file:
# - MINER_OPERATOR_KEYS: comma-separated operator private keys
# - LICENSE_PER_OPERATOR: number of licenses per operator
# - MINTER_KEY: minter's private key (secp256k1)
#
# For each operator:
# 1. Derives operator address
# 2. Mints the specified number of licenses

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
MINTER_KEY_NAME="minter"

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

# Set chain defaults
CHAIN_RPC="${CHAIN_RPC:-tcp://localhost:26657}"

# Validate required environment variables
if [ -z "$MINER_OPERATOR_KEYS" ]; then
  echo "Error: MINER_OPERATOR_KEYS is not set"
  exit 1
fi

if [ -z "$MINTER_KEY" ]; then
  echo "Error: MINTER_KEY is not set"
  exit 1
fi

LICENSE_COUNT=${LICENSE_PER_OPERATOR:-1}

# Parse operator keys array
IFS=',' read -ra OP_KEYS <<< "$MINER_OPERATOR_KEYS"
OPERATOR_COUNT=${#OP_KEYS[@]}

echo "=== License Minting ==="
echo "Operators: $OPERATOR_COUNT"
echo "Licenses per operator: $LICENSE_COUNT"
echo "Chain RPC: $CHAIN_RPC"
echo ""

# Import minter key to keyring
echo "Importing minter key to keyring..."
aultd keys delete "$MINTER_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -y 2>/dev/null || true

echo "$MINTER_KEY" | aultd keys unsafe-import-eth-key "$MINTER_KEY_NAME" /dev/stdin --keyring-backend "$KEYRING_BACKEND" 2>/dev/null || {
  echo "Trying alternative import method..."
  printf '%s' "$MINTER_KEY" | aultd keys unsafe-import-eth-key "$MINTER_KEY_NAME" - --keyring-backend "$KEYRING_BACKEND"
}

MINTER_ADDR=$(aultd keys show "$MINTER_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -a)
echo "Minter address: $MINTER_ADDR"
echo ""

# Process each operator
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OP_KEY="${OP_KEYS[$i]}"

  echo "--- Operator $((i+1))/$OPERATOR_COUNT ---"

  # Query operator address from chain using owner-key
  # First we need to derive address - use aultd if possible
  # Try to get address by importing temporarily
  TEMP_KEY_NAME="temp_op_$i"
  aultd keys delete "$TEMP_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -y 2>/dev/null || true

  echo "$OP_KEY" | aultd keys unsafe-import-eth-key "$TEMP_KEY_NAME" /dev/stdin --keyring-backend "$KEYRING_BACKEND" 2>/dev/null || {
    printf '%s' "$OP_KEY" | aultd keys unsafe-import-eth-key "$TEMP_KEY_NAME" - --keyring-backend "$KEYRING_BACKEND" 2>/dev/null
  } || {
    echo "Error: Failed to derive address for operator $((i+1))"
    continue
  }

  OPERATOR_ADDR=$(aultd keys show "$TEMP_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -a 2>/dev/null)
  aultd keys delete "$TEMP_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -y 2>/dev/null || true

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Error: Failed to get address for operator $((i+1))"
    continue
  fi

  echo "Operator Address: $OPERATOR_ADDR"

  # Mint licenses
  echo "Minting $LICENSE_COUNT license(s)..."
  for j in $(seq 1 $LICENSE_COUNT); do
    echo "  Minting license $j/$LICENSE_COUNT..."

    TXHASH=$(aultd tx license mint "$OPERATOR_ADDR" \
      "ipfs://miner-license-op$((i+1))-$j" \
      "miner setup via script" \
      --from "$MINTER_KEY_NAME" \
      --keyring-backend "$KEYRING_BACKEND" \
      --node "$CHAIN_RPC" \
      --gas auto \
      --gas-adjustment 1.3 \
      --yes \
      --output json 2>/dev/null | jq -r '.txhash // "pending"' || echo "pending")

    echo "    TX: $TXHASH"
    sleep 2
  done

  echo ""
done

echo "=== License Minting Complete ==="
echo ""
echo "Operators processed: $OPERATOR_COUNT"
echo "Licenses per operator: $LICENSE_COUNT"
echo "Total licenses minted: $((OPERATOR_COUNT * LICENSE_COUNT))"
