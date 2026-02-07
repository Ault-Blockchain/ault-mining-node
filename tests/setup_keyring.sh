#!/bin/bash

# Usage: ./setup_keyring.sh [--force]
#
# Imports operator and license holder keys to the keyring and caches the results.
# Other scripts can source the cached data instead of re-importing.
#
# Options:
#   --force    Force re-import even if cache exists

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYRING_BACKEND="test"
KEYRING_DIR="${HOME}/.aultd"
KEYRING_PASS="testpass"

# Cache directory
CACHE_DIR="$SCRIPT_DIR/.keyring_cache"

# Parse command line arguments
FORCE_REIMPORT=false
for arg in "$@"; do
  case $arg in
    --force)
      FORCE_REIMPORT=true
      shift
      ;;
  esac
done

# Load environment
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
else
  echo "Error: .env file not found"
  exit 1
fi

# Load operator mnemonics from env
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

# Load license holder mnemonics from env
declare -a LICENSE_HOLDER_MNEMONICS=()
i=0
while true; do
  eval val="\$LICENSE_HOLDER_MNEMONIC_${i}"
  if [ -z "$val" ]; then break; fi
  LICENSE_HOLDER_MNEMONICS+=("$val")
  i=$((i+1))
done
LICENSE_HOLDER_COUNT=${#LICENSE_HOLDER_MNEMONICS[@]}

# Common keyring flags
KEYRING_FLAGS="--keyring-backend $KEYRING_BACKEND --home $KEYRING_DIR"

# Check if cache is valid
cache_valid() {
  if [ "$FORCE_REIMPORT" = true ]; then
    return 1
  fi

  if [ ! -d "$CACHE_DIR" ]; then
    return 1
  fi

  # Check if operator count matches
  if [ ! -f "$CACHE_DIR/operator_count" ]; then
    return 1
  fi
  cached_op_count=$(cat "$CACHE_DIR/operator_count")
  if [ "$cached_op_count" != "$OPERATOR_COUNT" ]; then
    return 1
  fi

  # Check if license holder count matches
  if [ ! -f "$CACHE_DIR/holder_count" ]; then
    return 1
  fi
  cached_holder_count=$(cat "$CACHE_DIR/holder_count")
  if [ "$cached_holder_count" != "$LICENSE_HOLDER_COUNT" ]; then
    return 1
  fi

  # Check if all required files exist
  if [ ! -f "$CACHE_DIR/operator_addrs.txt" ] || [ ! -f "$CACHE_DIR/operator_keys.txt" ]; then
    return 1
  fi

  if [ $LICENSE_HOLDER_COUNT -gt 0 ] && [ ! -f "$CACHE_DIR/holder_addrs.txt" ]; then
    return 1
  fi

  return 0
}

# If called with --check, just verify cache and exit
if [ "$1" = "--check" ]; then
  if cache_valid; then
    echo "valid"
    exit 0
  else
    echo "invalid"
    exit 1
  fi
fi

# If cache is valid, just print status and exit
if cache_valid; then
  echo "Keyring cache is valid ($OPERATOR_COUNT operators, $LICENSE_HOLDER_COUNT holders)"
  echo "Use --force to re-import"
  exit 0
fi

echo "=== Setting up keyring ==="
echo "Operators: $OPERATOR_COUNT"
echo "License holders: $LICENSE_HOLDER_COUNT"
echo ""

# Clean and create cache directory
rm -rf "$CACHE_DIR"
mkdir -p "$CACHE_DIR"

# Import operator keys
echo "Importing operator keys..."
> "$CACHE_DIR/operator_addrs.txt"
> "$CACHE_DIR/operator_keys.txt"

for i in $(seq 0 $((OPERATOR_COUNT-1))); do
  MNEMONIC="${OPERATOR_MNEMONICS[$i]}"
  KEY_NAME="operator$i"

  # Delete existing key if any
  printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true

  # Import key
  echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

  # Get address
  OPERATOR_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)
  if [ -z "$OPERATOR_ADDR" ]; then
    echo "Error: Failed to get address for operator$i"
    exit 1
  fi
  echo "$OPERATOR_ADDR" >> "$CACHE_DIR/operator_addrs.txt"

  # Get private key (for VRF setup)
  OPERATOR_KEY=$(printf '%s\n' "$KEYRING_PASS" | aultd keys unsafe-export-eth-key $KEY_NAME $KEYRING_FLAGS 2>/dev/null)
  if [ -z "$OPERATOR_KEY" ]; then
    echo "Error: Failed to derive key for operator$i"
    exit 1
  fi
  echo "$OPERATOR_KEY" >> "$CACHE_DIR/operator_keys.txt"

  # Progress indicator
  if [ $(( (i + 1) % 20 )) -eq 0 ]; then
    echo "  Imported $((i+1))/$OPERATOR_COUNT operators"
  fi
done
echo "  Imported $OPERATOR_COUNT operators"

# Import license holder keys (if any)
if [ $LICENSE_HOLDER_COUNT -gt 0 ]; then
  echo "Importing license holder keys..."
  > "$CACHE_DIR/holder_addrs.txt"

  for i in $(seq 0 $((LICENSE_HOLDER_COUNT-1))); do
    MNEMONIC="${LICENSE_HOLDER_MNEMONICS[$i]}"
    KEY_NAME="licenseholder$i"

    # Delete existing key if any
    printf '%s\n%s\n' "$KEYRING_PASS" "$KEYRING_PASS" | aultd keys delete $KEY_NAME $KEYRING_FLAGS -y 2>/dev/null || true

    # Import key
    echo "$MNEMONIC" | aultd keys add $KEY_NAME --recover $KEYRING_FLAGS 2>/dev/null

    # Get address
    HOLDER_ADDR=$(printf '%s\n' "$KEYRING_PASS" | aultd keys show $KEY_NAME $KEYRING_FLAGS -a 2>/dev/null)
    if [ -z "$HOLDER_ADDR" ]; then
      echo "Error: Failed to get address for licenseholder$i"
      exit 1
    fi
    echo "$HOLDER_ADDR" >> "$CACHE_DIR/holder_addrs.txt"

    # Progress indicator
    if [ $(( (i + 1) % 10 )) -eq 0 ]; then
      echo "  Imported $((i+1))/$LICENSE_HOLDER_COUNT holders"
    fi
  done
  echo "  Imported $LICENSE_HOLDER_COUNT holders"
fi

# Save counts for cache validation
echo "$OPERATOR_COUNT" > "$CACHE_DIR/operator_count"
echo "$LICENSE_HOLDER_COUNT" > "$CACHE_DIR/holder_count"

echo ""
echo "=== Keyring setup complete ==="
echo "Cache saved to: $CACHE_DIR/"
echo ""
echo "Other scripts will now use cached data automatically."
