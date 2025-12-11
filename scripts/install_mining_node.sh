#!/bin/bash
set -e

# Usage: ./install_mining_node.sh [version]
# Example: ./install_mining_node.sh latest
# Example: ./install_mining_node.sh v1.0.0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Load environment if exists
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
fi

VERSION=${1:-latest}
IMAGE="${DOCKER_IMAGE:-ghcr.io/ault-blockchain/aultmined}"

echo "=== Mining Node Installation ==="
echo "Image: ${IMAGE}:${VERSION}"
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
  echo "Error: Docker not found"
  echo "Please install Docker first: https://docs.docker.com/get-docker/"
  exit 1
fi

echo "Docker found: $(docker --version)"
echo ""

# Try to pull Docker image
echo "Pulling ${IMAGE}:${VERSION}..."
PULL_OUTPUT=$(docker pull "${IMAGE}:${VERSION}" 2>&1) && PULL_SUCCESS=true || PULL_SUCCESS=false

if [ "$PULL_SUCCESS" = true ]; then
  echo ""
  echo "=== Installation Complete ==="
  echo "Docker image pulled successfully: ${IMAGE}:${VERSION}"
  echo ""
  echo "Next steps:"
  echo "  1. Copy .env.template to .env and configure"
  echo "  2. Run ./setup_vrf_key.sh to generate and register VRF key"
  echo "  3. Run ./start_miner_client.sh to start mining"
  exit 0
fi

echo ""

# Check if it's an authentication error
if echo "$PULL_OUTPUT" | grep -qi "unauthorized"; then
  echo "Authentication required for private registry."
  echo ""
  echo "To authenticate with GitHub Container Registry:"
  echo "  1. Create a Personal Access Token (PAT) at https://github.com/settings/tokens"
  echo "     - Select 'read:packages' scope"
  echo "  2. Run: echo <YOUR_PAT> | docker login ghcr.io -u <YOUR_GITHUB_USERNAME> --password-stdin"
  echo "  3. Re-run this script"
  echo ""
  read -p "Have you already logged in? Try pull again? (y/n): " RETRY_CHOICE
  if [[ "$RETRY_CHOICE" == "y" || "$RETRY_CHOICE" == "Y" ]]; then
    echo ""
    echo "Retrying pull..."
    if docker pull "${IMAGE}:${VERSION}"; then
      echo ""
      echo "=== Installation Complete ==="
      echo "Docker image pulled successfully: ${IMAGE}:${VERSION}"
      exit 0
    fi
  fi
  echo ""
fi

echo "Failed to pull Docker image."
echo ""

# Ask user if they want to build locally
read -p "Do you want to build from source? (y/n): " BUILD_CHOICE

if [[ "$BUILD_CHOICE" != "y" && "$BUILD_CHOICE" != "Y" ]]; then
  echo "Installation cancelled."
  exit 0
fi

echo ""
echo "Building from source..."
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
  echo "Error: Go not found"
  echo "Please install Go first: https://go.dev/doc/install"
  exit 1
fi

echo "Go found: $(go version)"
echo ""

# Build from source
cd "$PROJECT_ROOT"

if [ -f "Makefile" ]; then
  echo "Running make build..."
  make build
else
  echo "Running go build..."
  go build -trimpath -ldflags "-s -w" -o aultmined ./cmd
fi

echo ""
echo "=== Installation Complete ==="
echo "Binary built: ${PROJECT_ROOT}/aultmined"
echo ""
echo "Next steps:"
echo "  1. Set environment variables (MINER_OPERATOR_KEY, MINER_VRF_KEY, etc.)"
echo "  2. Run ./setup_vrf_key.sh to generate and register VRF key"
echo "  3. Run ./aultmined mine --yes to start mining"
