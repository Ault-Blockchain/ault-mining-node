#!/bin/bash
set -e

# Usage: ./start_miner_client.sh [version]
# Example: ./start_miner_client.sh latest
# Example: ./start_miner_client.sh v1.0.0
#
# Miner count is determined by MINER_OPERATOR_KEYS array size
# Port mapping: miner-1 -> 8080, miner-2 -> 8081, etc.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

VERSION=${1:-latest}
IMAGE="${DOCKER_IMAGE:-ghcr.io/ault-blockchain/aultmined}"

# Parse arrays (comma-separated)
IFS=',' read -ra OP_KEYS <<< "$MINER_OPERATOR_KEYS"
IFS=',' read -ra VRF_KEYS <<< "$MINER_VRF_KEYS"
COUNT=${#OP_KEYS[@]}

if [ $COUNT -eq 0 ]; then
  echo "Error: MINER_OPERATOR_KEYS is empty"
  exit 1
fi

if [ ${#VRF_KEYS[@]} -ne $COUNT ]; then
  echo "Error: MINER_OPERATOR_KEYS and MINER_VRF_KEYS count mismatch"
  echo "  MINER_OPERATOR_KEYS count: $COUNT"
  echo "  MINER_VRF_KEYS count: ${#VRF_KEYS[@]}"
  exit 1
fi

echo "=== Starting Miner Clients ==="
echo "Image: ${IMAGE}:${VERSION}"
echo "Miner count: $COUNT"
echo "Chain: $CHAIN_ID"
echo ""

# Always pull latest image before running
echo "Pulling ${IMAGE}:${VERSION}..."
docker pull "${IMAGE}:${VERSION}"

echo ""

# Stop and remove existing containers
echo "Cleaning up existing containers..."
for i in $(seq 1 $COUNT); do
  docker rm -f "miner-$i" 2>/dev/null || true
done

echo ""

# Start miners with individual keys injected
for i in $(seq 1 $COUNT); do
  idx=$((i-1))
  PORT=$((8079+i))

  echo "Starting miner-$i (port: $PORT)..."
  docker run -d \
    --name "miner-$i" \
    --restart unless-stopped \
    -e MINER_OPERATOR_KEY="${OP_KEYS[$idx]}" \
    -e MINER_VRF_KEY="${VRF_KEYS[$idx]}" \
    -e CHAIN_GRPC="${CHAIN_GRPC}" \
    -e CHAIN_RPC="${CHAIN_RPC}" \
    -e CHAIN_ID="${CHAIN_ID}" \
    -e MINER_API_PORT=8080 \
    -e MINER_BATCH_SIZE="${MINER_BATCH_SIZE:-100}" \
    -p "${PORT}:8080" \
    -v "miner${i}_data:/app/data" \
    "${IMAGE}:${VERSION}" \
    mine --yes
done

echo ""
echo "=== Started $COUNT miner(s) ==="
echo ""
docker ps --filter "name=miner-" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
echo ""
echo "To view logs: docker logs -f miner-1"
echo "To stop all: for i in \$(seq 1 $COUNT); do docker stop miner-\$i; done"
