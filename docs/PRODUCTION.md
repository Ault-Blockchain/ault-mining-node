# Production Deployment Guide

## Prerequisites

- **AULT Account**: Account with AULT tokens for gas fees
- **Mining License(s)**: Owned or delegated mining licenses
- **Access to Ault Node**: gRPC and RPC endpoints

## Installation

```bash
git clone https://github.com/Ault-Blockchain/ault.git
cd ault/miner
make install
```

## Setup

### 1. Generate VRF Key

Generate Ed25519 keypair for VRF (Verifiable Random Function) proof generation.

```bash
aultmined vrfkeygen
```

Output:
- **Private Key** (64 bytes hex) → Set as `MINER_VRF_KEY`
- **Public Key** (32 bytes hex) → Registered on-chain

### 2. Environment Variables

Configure miner with required keys and network endpoints.

```bash
# Required
MINER_OPERATOR_KEY=<your-account-private-key-hex>  # secp256k1, for signing txs
MINER_VRF_KEY=<generated-vrf-private-key-hex>      # Ed25519, from vrfkeygen

# Network
MINER_GRPC_ENDPOINT=your-node:9090
MINER_RPC_ENDPOINT=http://your-node:26657

# Optional
MINER_API_PORT=8080
MINER_DB_PATH=./miner.db
MINER_LOG_LEVEL=info
```

### 3. Register VRF Key

Submit VRF public key to chain. Required before mining - chain verifies proofs against this key.

```bash
aultmined set-key
```

### 4. Start Mining

Begin continuous mining. Auto-detects owned and delegated licenses.

```bash
aultmined mine --yes
```

## Systemd Service Example

`/etc/systemd/system/aultmined.service`:

```ini
[Unit]
Description=Ault Miner Client
After=network-online.target

[Service]
Type=simple
EnvironmentFile=/path/to/.env
ExecStart=/usr/local/bin/aultmined mine --yes
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable aultmined
sudo systemctl start aultmined
journalctl -u aultmined -f
```

## Docker Example

```bash
docker run -d \
  --name ault-miner \
  --restart unless-stopped \
  -e MINER_OPERATOR_KEY="<key>" \
  -e MINER_VRF_KEY="<vrf-key>" \
  -e MINER_GRPC_ENDPOINT="host.docker.internal:9090" \
  -p 8080:8080 \
  ault-miner
```

