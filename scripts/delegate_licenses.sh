#!/bin/bash
set -e

# Usage: ./delegate_licenses.sh
#
# Delegates licenses from LICENSE_HOLDER to 4 operators (equal distribution)
#
# Required .env variables:
# - LICENSE_HOLDER_KEY: license holder's private key (must own the licenses)
# - MINER_OPERATOR_KEYS: comma-separated operator private keys (4 operators)

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
DELEGATION_BATCH_SIZE=25

# Validate required environment variables
if [ -z "$LICENSE_HOLDER_KEY" ]; then
  echo "Error: LICENSE_HOLDER_KEY is not set"
  exit 1
fi

if [ -z "$MINER_OPERATOR_KEYS" ]; then
  echo "Error: MINER_OPERATOR_KEYS is not set"
  exit 1
fi

# Parse operator keys array
IFS=',' read -ra OP_KEYS <<< "$MINER_OPERATOR_KEYS"
OPERATOR_COUNT=${#OP_KEYS[@]}

if [ $OPERATOR_COUNT -lt 1 ]; then
  echo "Error: At least 1 operator required"
  exit 1
fi

# Common keyring flags (used throughout script)
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

echo "=== License Delegation Setup ==="
echo "Operators: $OPERATOR_COUNT"
echo "Chain RPC: $CHAIN_RPC"
echo "Chain ID: $CHAIN_ID"
echo ""

# Step 1: Import keys to keyring
echo "Step 1: Import keys to keyring"

# Import license holder key
echo "  Importing license holder key..."
printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete licenseholder $KEYRING_FLAGS -y 2>/dev/null || true
printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys unsafe-import-eth-key licenseholder "$LICENSE_HOLDER_KEY" $KEYRING_FLAGS 2>/dev/null

LICENSE_HOLDER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show licenseholder $KEYRING_FLAGS -a 2>/dev/null)
if [ -z "$LICENSE_HOLDER_ADDR" ]; then
  echo "Error: Failed to import license holder key"
  exit 1
fi
echo "  License holder address: $LICENSE_HOLDER_ADDR"

# Derive operator addresses
echo "  Deriving operator addresses..."
declare -a OPERATOR_ADDRS=()
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  OP_KEY="${OP_KEYS[$i]}"
  TEMP_KEY_NAME="temp_op_$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $TEMP_KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys unsafe-import-eth-key $TEMP_KEY_NAME "$OP_KEY" $KEYRING_FLAGS 2>/dev/null

  OPERATOR_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $TEMP_KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $TEMP_KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Error: Failed to get address for operator $((i+1))"
    exit 1
  fi

  OPERATOR_ADDRS+=("$OPERATOR_ADDR")
  echo "    Operator $((i+1)): $OPERATOR_ADDR"
done

echo ""

# Step 2: Query owned licenses
echo "Step 2: Query owned licenses"
OWNED_LICENSES=$(aultd q license owned-by "$LICENSE_HOLDER_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null)
LICENSE_IDS=($(echo "$OWNED_LICENSES" | jq -r '.license_ids[]'))
TOTAL_LICENSES=${#LICENSE_IDS[@]}

echo "  License holder owns: $TOTAL_LICENSES licenses"

if [ $TOTAL_LICENSES -eq 0 ]; then
  echo "Error: No licenses to delegate. Run mint_licenses.sh first."
  exit 1
fi

# Calculate licenses per operator
LICENSES_PER_OPERATOR=$((TOTAL_LICENSES / OPERATOR_COUNT))
REMAINDER=$((TOTAL_LICENSES % OPERATOR_COUNT))

echo "  Licenses per operator: $LICENSES_PER_OPERATOR"
if [ $REMAINDER -gt 0 ]; then
  echo "  Remainder licenses (goes to last operator): $REMAINDER"
fi

echo ""

# Step 3: Delegate licenses to operators
echo "Step 3: Delegate licenses to operators"

LICENSE_OFFSET=0
for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
  OPERATOR_ADDR="${OPERATOR_ADDRS[$op_idx]}"

  # Calculate how many licenses for this operator
  OP_LICENSE_COUNT=$LICENSES_PER_OPERATOR
  if [ $op_idx -eq $((OPERATOR_COUNT-1)) ]; then
    # Last operator gets remainder
    OP_LICENSE_COUNT=$((LICENSES_PER_OPERATOR + REMAINDER))
  fi

  OP_LICENSE_IDS=("${LICENSE_IDS[@]:$LICENSE_OFFSET:$OP_LICENSE_COUNT}")
  LICENSE_OFFSET=$((LICENSE_OFFSET + OP_LICENSE_COUNT))

  echo ""
  echo "  --- Operator $((op_idx+1)): $OPERATOR_ADDR ---"
  echo "  Delegating ${#OP_LICENSE_IDS[@]} licenses..."

  # Delegate in batches
  for ((i=0; i<${#OP_LICENSE_IDS[@]}; i+=DELEGATION_BATCH_SIZE)); do
    BATCH_IDS=("${OP_LICENSE_IDS[@]:i:DELEGATION_BATCH_SIZE}")
    BATCH_CSV=$(IFS=,; echo "${BATCH_IDS[*]}")

    TX_RESULT=$(aultd tx miner delegate-mining "$BATCH_CSV" "$OPERATOR_ADDR" \
      --from licenseholder \
      $KEYRING_FLAGS \
      --node "$CHAIN_RPC" \
      --chain-id "$CHAIN_ID" \
      --gas 2000000 \
      --fees 5000000000000000aault \
      --fee-granter "$FEEGRANT_MODULE_ADDR" \
      --broadcast-mode sync \
      --yes \
      --output json 2>&1)

    TX_HASH=$(echo "$TX_RESULT" | jq -r '.txhash // empty')
    TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // 0')

    if [ -z "$TX_HASH" ] || [ "$TX_CODE" != "0" ]; then
      echo "  Failed to delegate licenses to operator $((op_idx+1)) batch $((i/DELEGATION_BATCH_SIZE + 1)) (code: $TX_CODE)"
      echo "$TX_RESULT"
      exit 1
    fi

    printf "    Batch %d: TX %s..." "$((i/DELEGATION_BATCH_SIZE + 1))" "$TX_HASH"

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
done

echo ""
echo "=== Delegation Complete ==="
echo ""
echo "Summary:"
echo "  License holder: $LICENSE_HOLDER_ADDR"
echo "  Total licenses delegated: $TOTAL_LICENSES"
echo ""
echo "Delegations:"
LICENSE_OFFSET=0
for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
  OP_LICENSE_COUNT=$LICENSES_PER_OPERATOR
  if [ $op_idx -eq $((OPERATOR_COUNT-1)) ]; then
    OP_LICENSE_COUNT=$((LICENSES_PER_OPERATOR + REMAINDER))
  fi
  echo "  Operator $((op_idx+1)) (${OPERATOR_ADDRS[$op_idx]}): $OP_LICENSE_COUNT licenses"
done
echo ""
echo "Note: Delegations will be active from the next epoch"
