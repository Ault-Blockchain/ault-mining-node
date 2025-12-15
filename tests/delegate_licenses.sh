#!/bin/bash

# Usage: ./delegate_licenses.sh
#
# Delegates licenses from license holders to 4 operators
# Reads license holder info from license_holders.csv (created by mint_licenses.sh)

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
DELEGATION_BATCH_SIZE=500

# Check for license_holders.csv
LICENSE_CSV="$SCRIPT_DIR/license_holders.csv"
if [ ! -f "$LICENSE_CSV" ]; then
  echo "Error: $LICENSE_CSV not found"
  echo "Please run mint_licenses.sh first"
  exit 1
fi

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

# Read CSV into arrays (skip header)
declare -a HOLDER_KEYS=()
declare -a HOLDER_ADDRS=()
declare -a FIRST_LICENSES=()
declare -a LAST_LICENSES=()
declare -a LICENSE_COUNTS=()

while IFS=',' read -r holder_idx holder_key holder_addr first_license last_license count; do
  HOLDER_KEYS+=("$holder_key")
  HOLDER_ADDRS+=("$holder_addr")
  FIRST_LICENSES+=("$first_license")
  LAST_LICENSES+=("$last_license")
  LICENSE_COUNTS+=("$count")
done < <(tail -n +2 "$LICENSE_CSV")

LICENSE_HOLDER_COUNT=${#HOLDER_KEYS[@]}

echo "=== License Delegation Setup ==="
echo "License CSV: $LICENSE_CSV"
echo "License holders: $LICENSE_HOLDER_COUNT"
echo "Operators: $OPERATOR_COUNT"
echo "Batch size: $DELEGATION_BATCH_SIZE"
echo "Chain RPC: $CHAIN_RPC"
echo "Chain ID: $CHAIN_ID"
echo ""

# Step 1: Import keys
echo "Step 1: Import keys"

# Import license holder keys
echo "  Importing license holder keys..."
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

# Step 3: Delegate licenses from each holder to operators
echo "Step 3: Delegate licenses from each holder to operators"

for holder_idx in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
  holder_key="${HOLDER_KEYS[$holder_idx]}"
  holder_addr="${HOLDER_ADDRS[$holder_idx]}"
  first_license="${FIRST_LICENSES[$holder_idx]}"
  last_license="${LAST_LICENSES[$holder_idx]}"
  count="${LICENSE_COUNTS[$holder_idx]}"

  echo ""
  echo "  --- $holder_key ($holder_addr) ---"
  echo "  Licenses: $first_license - $last_license ($count total)"

  # Calculate licenses per operator for this holder
  LICENSES_PER_OP=$((count / OPERATOR_COUNT))

  LICENSE_OFFSET=0
  for op_idx in $(seq 0 $((OPERATOR_COUNT-1))); do
    OPERATOR_ADDR="${OPERATOR_ADDRS[$op_idx]}"

    # Calculate license range for this operator
    START_ID=$((first_license + LICENSE_OFFSET))
    END_ID=$((START_ID + LICENSES_PER_OP - 1))

    # Delegate in batches
    for ((batch_start=START_ID; batch_start<=END_ID; batch_start+=DELEGATION_BATCH_SIZE)); do
      batch_end=$((batch_start + DELEGATION_BATCH_SIZE - 1))
      if [ $batch_end -gt $END_ID ]; then
        batch_end=$END_ID
      fi

      BATCH_COUNT=$((batch_end - batch_start + 1))
      echo "    Delegating $batch_start-$batch_end ($BATCH_COUNT) to Operator $((op_idx+1))..."

      BATCH_CSV=$(seq $batch_start $batch_end | tr '\n' ',' | sed 's/,$//')

      TX_RESULT=$(echo "y" | aultd tx miner delegate-mining "$BATCH_CSV" "$OPERATOR_ADDR" \
        --from "$holder_key" \
        --keyring-backend "$KEYRING_BACKEND" \
        --home "$KEYRING_DIR" \
        --node "$CHAIN_RPC" \
        --chain-id "$CHAIN_ID" \
        --gas 10000000 \
        --fees 5000000000000000aault \
        --fee-granter "$FEEGRANT_MODULE_ADDR" \
        --broadcast-mode sync \
        --yes \
        --output json 2>&1) || true

      TX_HASH=$(echo "$TX_RESULT" | jq -r '.txhash // empty' 2>/dev/null)
      TX_CODE=$(echo "$TX_RESULT" | jq -r '.code // 0' 2>/dev/null)

      if [ -z "$TX_HASH" ] || [ "$TX_CODE" != "0" ]; then
        echo "    Failed to delegate (code: $TX_CODE)"
        echo "$TX_RESULT"
        exit 1
      fi

      printf "    -> TX: %s\n" "$TX_HASH"

      sleep 2
    done

    LICENSE_OFFSET=$((LICENSE_OFFSET + LICENSES_PER_OP))
  done
done

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
