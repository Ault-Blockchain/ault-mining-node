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
# 1. Checks if VRF key is already registered
# 2. If not, generates and registers a new VRF key
# 3. Mints the specified number of licenses

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
IFS=',' read -ra VRF_KEYS <<< "$MINER_VRF_KEYS"
OPERATOR_COUNT=${#OP_KEYS[@]}

echo "=== Setup Licenses ==="
echo "Operators: $OPERATOR_COUNT"
echo "Licenses per operator: $LICENSE_COUNT"
echo ""

# Import minter key to keyring
echo "Importing minter key to keyring..."
# Remove existing key if exists
aultd keys delete "$MINTER_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -y 2>/dev/null || true

# Import key using echo and recover (hex private key)
echo "$MINTER_KEY" | aultd keys unsafe-import-eth-key "$MINTER_KEY_NAME" /dev/stdin --keyring-backend "$KEYRING_BACKEND" 2>/dev/null || {
  # Fallback: try using echo pipe directly
  echo "Trying alternative import method..."
  printf '%s' "$MINTER_KEY" | aultd keys unsafe-import-eth-key "$MINTER_KEY_NAME" - --keyring-backend "$KEYRING_BACKEND"
}

MINTER_ADDR=$(aultd keys show "$MINTER_KEY_NAME" --keyring-backend "$KEYRING_BACKEND" -a)
echo "Minter address: $MINTER_ADDR"
echo ""

# Array to store generated VRF keys
declare -a NEW_VRF_KEYS=()

# Process each operator
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OP_KEY="${OP_KEYS[$i]}"
  EXISTING_VRF="${VRF_KEYS[$i]:-}"

  # Derive operator address from private key
  OPERATOR_ADDR=$(MINER_OPERATOR_KEY="$OP_KEY" aultmined address 2>/dev/null || echo "")

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Error: Failed to derive address for operator $((i+1))"
    echo "Trying alternative method..."
    OPERATOR_ADDR="operator-$((i+1))"
  fi

  echo "--- Operator $((i+1)): $OPERATOR_ADDR ---"

  # Check if VRF key is already registered
  echo "Checking VRF key registration..."
  VRF_RESULT=$(aultd q miner owner-key "$OPERATOR_ADDR" --output json 2>/dev/null || echo "{}")
  REGISTERED_VRF=$(echo "$VRF_RESULT" | jq -r '.vrf_key // empty')

  if [ -z "$REGISTERED_VRF" ] || [ "$REGISTERED_VRF" == "null" ]; then
    echo "VRF key not registered"

    # Check if we have a VRF key in the array
    if [ -n "$EXISTING_VRF" ]; then
      echo "Using existing VRF key from MINER_VRF_KEYS"
      VRF_PRIVATE_KEY="$EXISTING_VRF"
    else
      # Generate new VRF key
      echo "Generating new VRF key..."
      VRF_OUTPUT=$(aultmined vrfkeygen)
      VRF_PRIVATE_KEY=$(echo "$VRF_OUTPUT" | grep -i "private" | awk '{print $NF}')
      VRF_PUBLIC_KEY=$(echo "$VRF_OUTPUT" | grep -i "public" | awk '{print $NF}')

      if [ -z "$VRF_PRIVATE_KEY" ]; then
        echo "Error: Failed to generate VRF key"
        exit 1
      fi

      echo "Generated VRF Public Key: $VRF_PUBLIC_KEY"
    fi

    NEW_VRF_KEYS+=("$VRF_PRIVATE_KEY")

    # Register VRF key on-chain
    echo "Registering VRF key on-chain..."
    MINER_OPERATOR_KEY="$OP_KEY" \
    MINER_VRF_KEY="$VRF_PRIVATE_KEY" \
    CHAIN_GRPC="$CHAIN_GRPC" \
    CHAIN_RPC="$CHAIN_RPC" \
    CHAIN_ID="$CHAIN_ID" \
    aultmined set-key

    echo "VRF key registered"
  else
    echo "VRF key already registered: $REGISTERED_VRF"
    NEW_VRF_KEYS+=("${EXISTING_VRF:-already-registered}")
  fi

  # Mint licenses
  echo "Minting $LICENSE_COUNT license(s)..."
  for j in $(seq 1 $LICENSE_COUNT); do
    echo "  Minting license $j/$LICENSE_COUNT..."

    TXHASH=$(aultd tx license mint "$OPERATOR_ADDR" \
      "ipfs://miner-license-op$((i+1))-$j" \
      "miner setup via script" \
      --from "$MINTER_KEY_NAME" \
      --keyring-backend "$KEYRING_BACKEND" \
      --gas auto \
      --gas-adjustment 1.3 \
      --yes \
      --output json 2>/dev/null | jq -r '.txhash // "pending"' || echo "pending")

    echo "    TX: $TXHASH"
    sleep 2
  done

  echo ""
done

echo "=== Setup Complete ==="
echo ""
echo "Operators processed: $OPERATOR_COUNT"
echo "Licenses per operator: $LICENSE_COUNT"
echo ""

# Output new VRF keys if any were generated
if [ ${#NEW_VRF_KEYS[@]} -gt 0 ]; then
  echo "Update your .env with these VRF keys:"
  echo "MINER_VRF_KEYS=$(IFS=','; echo "${NEW_VRF_KEYS[*]}")"
fi
