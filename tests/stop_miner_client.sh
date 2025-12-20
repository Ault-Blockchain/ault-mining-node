#!/bin/bash
set -e

# Stop all miner Docker containers

echo "=== Stopping Miner Clients ==="

CONTAINERS=$(docker ps -a --filter "name=miner-" --format "{{.Names}}" 2>/dev/null || true)

if [ -z "$CONTAINERS" ]; then
  echo "No miner containers found"
  exit 0
fi

COUNT=0
for name in $CONTAINERS; do
  echo "Stopping $name..."
  docker stop "$name" 2>/dev/null || true
  docker rm "$name" 2>/dev/null || true
  COUNT=$((COUNT+1))
done

echo ""
echo "=== Stopped $COUNT miner(s) ==="
