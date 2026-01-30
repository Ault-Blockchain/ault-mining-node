#!/bin/bash

# Usage: ./check_vrf_keys.sh
#
# Checks if all operators have VRF keys registered on-chain

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

# Common keyring flags
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

# Load operator mnemonics from env (OPERATOR_MNEMONIC_0, OPERATOR_MNEMONIC_1, ...)
declare -a OPERATOR_MNEMONICS=()
i=0
while true; do
  eval val="\$OPERATOR_MNEMONIC_${i}"
  if [ -z "$val" ]; then break; fi
  OPERATOR_MNEMONICS+=("$val")
  i=$((i+1))
done

if [ ${#OPERATOR_MNEMONICS[@]} -eq 0 ]; then
  echo "Error: No OPERATOR_MNEMONIC_* variables found in .env"
  exit 1
fi
OPERATOR_COUNT=${#OPERATOR_MNEMONICS[@]}

echo "=== VRF Key Check ==="
echo "Operators: $OPERATOR_COUNT"
echo "Chain RPC: $CHAIN_RPC"
echo ""

# Import keys and check VRF registration
ALL_OK=true
for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  MNEMONIC="${OPERATOR_MNEMONICS[$i]}"
  KEY_NAME="operator$i"

  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  OPERATOR_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)

  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Operator $i: Failed to get address"
    ALL_OK=false
    continue
  fi

  QUERY_RESULT=$(aultd q miner owner-key "$OPERATOR_ADDR" --node "$CHAIN_RPC" --output json 2>/dev/null)
  VRF_PUBKEY=$(echo "$QUERY_RESULT" | jq -r '.vrf_pubkey // empty' 2>/dev/null)

  if [ -z "$VRF_PUBKEY" ] || [ "$VRF_PUBKEY" = "null" ]; then
    echo "Operator $i ($OPERATOR_ADDR): NOT REGISTERED"
    ALL_OK=false
  else
    EPOCH=$(echo "$QUERY_RESULT" | jq -r '.registration_epoch // empty' 2>/dev/null)
    VALID_FROM=$(echo "$QUERY_RESULT" | jq -r '.valid_from_epoch // empty' 2>/dev/null)

    # Check if .env VRF key matches on-chain pubkey
    eval ENV_VRF_KEY="\$MINER_VRF_KEY_${i}"
    KEY_MATCH=""
    if [ -n "$ENV_VRF_KEY" ]; then
      # Extract public key (last 32 bytes = last 64 hex chars) and convert to base64
      LOCAL_PUBKEY_HEX="${ENV_VRF_KEY: -64}"
      LOCAL_PUBKEY_B64=$(echo "$LOCAL_PUBKEY_HEX" | xxd -r -p | base64)
      if [ "$LOCAL_PUBKEY_B64" = "$VRF_PUBKEY" ]; then
        KEY_MATCH="env=MATCH"
      else
        KEY_MATCH="env=MISMATCH (local=${LOCAL_PUBKEY_B64})"
        ALL_OK=false
      fi
    else
      KEY_MATCH="env=NO_KEY"
      ALL_OK=false
    fi

    echo "Operator $i ($OPERATOR_ADDR): OK (pubkey=${VRF_PUBKEY:0:20}... epoch=$EPOCH valid_from=$VALID_FROM $KEY_MATCH)"
  fi
done

echo ""
if [ "$ALL_OK" = true ]; then
  echo "All $OPERATOR_COUNT operators have VRF keys registered."
else
  echo "Some operators are missing VRF keys. Run ./setup_vrf_key.sh to register."
  exit 1
fi
