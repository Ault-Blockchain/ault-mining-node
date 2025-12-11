#!/bin/bash
set -e

# Usage: ./setup_vrf_key.sh <operator_private_key>
#    or: OPERATOR_KEY=<hex> ./setup_vrf_key.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load environment if exists (for chain config)
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
fi

# Get operator key from argument or environment
OPERATOR_KEY="${1:-$OPERATOR_KEY}"

if [ -z "$OPERATOR_KEY" ]; then
  echo "Error: Operator private key is required"
  echo ""
  echo "Usage:"
  echo "  ./setup_vrf_key.sh <operator_private_key_hex>"
  echo "  OPERATOR_KEY=<hex> ./setup_vrf_key.sh"
  exit 1
fi

# Set chain defaults if not provided
CHAIN_GRPC="${CHAIN_GRPC:-localhost:9090}"
CHAIN_RPC="${CHAIN_RPC:-tcp://localhost:26657}"
CHAIN_ID="${CHAIN_ID:-ault_4400-1}"

echo "=== VRF Key Setup ==="
echo "Chain ID: $CHAIN_ID"
echo "Chain gRPC: $CHAIN_GRPC"
echo "Chain RPC: $CHAIN_RPC"
echo ""

# Generate VRF key
echo "Generating VRF key..."
VRF_OUTPUT=$(aultmined vrfkeygen)

# Parse keys - they are on the line after "Private Key" and "Public Key" labels
VRF_PRIVATE_KEY=$(echo "$VRF_OUTPUT" | grep -A1 "Private Key" | tail -1 | tr -d '[:space:]')
VRF_PUBLIC_KEY=$(echo "$VRF_OUTPUT" | grep -A1 "Public Key" | tail -1 | tr -d '[:space:]')

if [ -z "$VRF_PRIVATE_KEY" ] || [ -z "$VRF_PUBLIC_KEY" ]; then
  echo "Error: Failed to parse VRF key"
  echo "Raw output:"
  echo "$VRF_OUTPUT"
  exit 1
fi

echo ""
echo "VRF Private Key: $VRF_PRIVATE_KEY"
echo "VRF Public Key:  $VRF_PUBLIC_KEY"
echo ""

# Register VRF key on chain
echo "Registering VRF key on chain..."
SET_KEY_OUTPUT=$(MINER_OPERATOR_KEY="$OPERATOR_KEY" \
MINER_VRF_KEY="$VRF_PRIVATE_KEY" \
CHAIN_GRPC="$CHAIN_GRPC" \
CHAIN_RPC="$CHAIN_RPC" \
CHAIN_ID="$CHAIN_ID" \
aultmined set-key 2>&1) || true

echo "$SET_KEY_OUTPUT"

# Parse operator address from set-key output
OPERATOR_ADDR=$(echo "$SET_KEY_OUTPUT" | grep -i "Setting VRF key for owner" | awk '{print $NF}')
if [ -z "$OPERATOR_ADDR" ]; then
  echo ""
  echo "Warning: Could not parse operator address from output"
  echo "VRF key may have been registered. Please verify manually."
  echo ""
  echo "=== VRF Key Setup Complete ==="
  echo ""
  echo "Save these keys for your .env configuration:"
  echo "  MINER_OPERATOR_KEY=$OPERATOR_KEY"
  echo "  MINER_VRF_KEY=$VRF_PRIVATE_KEY"
  exit 0
fi

echo ""
echo "Operator Address: $OPERATOR_ADDR"

# Wait for transaction to be included
echo ""
echo "Waiting for transaction to be included..."
sleep 5

# Query to verify registration
echo "Verifying VRF key registration..."
QUERY_RESULT=$(aultd q miner owner-key "$OPERATOR_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null || echo "{}")
REGISTERED_VRF=$(echo "$QUERY_RESULT" | jq -r '.vrf_key // empty')

if [ -z "$REGISTERED_VRF" ] || [ "$REGISTERED_VRF" == "null" ]; then
  echo "Warning: Could not verify VRF key registration"
  echo "Please check manually: aultd q miner owner-key $OPERATOR_ADDR"
else
  echo "VRF key registered successfully!"
  echo "Registered VRF Public Key: $REGISTERED_VRF"
fi

echo ""
echo "=== VRF Key Setup Complete ==="
echo ""
echo "Save these keys for your .env configuration:"
echo "  MINER_OPERATOR_KEY=$OPERATOR_KEY"
echo "  MINER_VRF_KEY=$VRF_PRIVATE_KEY"
