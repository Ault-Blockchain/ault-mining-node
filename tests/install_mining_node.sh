#!/bin/bash
set -e

# Usage: ./install_mining_node.sh [--local] [version]
# Example: ./install_mining_node.sh latest
# Example: ./install_mining_node.sh --local
# Example: ./install_mining_node.sh v1.0.0
#
# Options:
#   --local    Build local Docker image (aultmined:local) instead of pulling from registry

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Parse command line arguments
USE_LOCAL=false
VERSION=""
for arg in "$@"; do
  case $arg in
    --local)
      USE_LOCAL=true
      ;;
    *)
      VERSION="$arg"
      ;;
  esac
done
VERSION=${VERSION:-latest}

# Load environment if exists
if [ -f "$SCRIPT_DIR/.env" ]; then
  source "$SCRIPT_DIR/.env"
elif [ -f ".env" ]; then
  source ".env"
fi

# Handle --local option: build local image
if [ "$USE_LOCAL" = true ]; then
  echo "=== Building Local Docker Image ==="
  echo "Building aultmined:local from source..."
  echo ""

  cd "$PROJECT_ROOT"

  # Build with GitHub PAT secret if available (required for private dependencies)
  if [ -n "$GH_PAT" ]; then
    echo "Using GH_PAT for private repository access..."
    docker build --secret id=GH_PAT,env=GH_PAT -t aultmined:local .
  else
    echo "Warning: GH_PAT not set. Build may fail for private dependencies."
    echo "Set GH_PAT environment variable: export GH_PAT=your_github_token"
    echo ""
    docker build -t aultmined:local .
  fi

  echo ""
  echo "=== Installation Complete ==="
  echo "Local Docker image built: aultmined:local"
  echo ""
  echo "Use --local flag with other scripts:"
  echo "  ./setup_vrf_key.sh --local"
  echo "  ./start_miner_client.sh --local"
  exit 0
fi

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
  echo "  2. Run ./mint_licenses.sh to mint licenses"
  echo "  3. Run ./setup_vrf_key.sh to generate and register VRF key"
  echo "  4. Run ./delegate_licenses.sh to delegate licenses to operators"
  echo "  5. Run ./start_miner_client.sh to start mining"
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
echo "  1. Copy .env.template to .env and configure"
echo "  2. Run ./mint_licenses.sh to mint licenses"
echo "  3. Run ./setup_vrf_key.sh to generate and register VRF key"
echo "  4. Run ./delegate_licenses.sh to delegate licenses to operators"
echo "  5. Run ./aultmined mine --yes to start mining"
