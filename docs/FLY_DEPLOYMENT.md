# Fly.io Deployment Guide

One-click deployment with automatic key generation. No binary download. No secrets required.

## Security Model

This guide uses a **delegation-based approach** where:

- Your main wallet (with AULT and licenses) is **NEVER** exposed
- A fresh "operator wallet" is auto-generated on Fly.io
- You delegate mining rights from your main wallet to the operator
- Mining rewards still go to your license holders
- Revoke delegation anytime to regain control

Note: Auto mode only mines **delegated** licenses; transferring license ownership to the operator is not supported.

## Prerequisites

- [Fly.io account](https://fly.io/app/sign-up) with `flyctl` installed
- Access to Ault testnet or mainnet
- One or more mining licenses owned by your main wallet
- `aultd` CLI for on-chain transactions (delegation)

## Quick Start

### Step 1: Clone and Launch

```bash
git clone https://github.com/Ault-Blockchain/ault-mining-node.git
cd ault-mining-node

# Launch Fly app (first time only)
fly launch --no-deploy
```

### Step 2: Deploy

```bash
fly deploy
```

A 4GB persistent volume is automatically created on first deploy to store keys.

The miner will:

1. Auto-generate an operator wallet and VRF key
2. Save keys to the persistent volume
3. Start healthy and wait for license delegation

### Step 3: Get Your Operator Address

```bash
curl https://<your-app>.fly.dev/v1/operator
```

Response:

```json
{
  "OperatorAddress": "ault1abc123...",
  "EVMAddress": "0xABC123...",
  "VRFPubKeyHex": "deadbeef...",
  "VRFRegistered": false,
  "LicenseCount": 0,
  "Licenses": [],
  "Status": "awaiting_delegation",
  "NextStep": "Delegate a license to this operator address"
}
```

**Copy the `OperatorAddress`** for the next step.

### Step 4: Delegate Licenses

From your **main wallet**, delegate your mining licenses to the operator:

```bash
# Get your license IDs first
aultd q license owned-by <your-main-wallet-address> \
  --node https://test-rpc.cloud.aultblockchain.xyz

# Delegate licenses to the operator address
aultd tx miner delegate-mining <operator-address> <license-id-1> <license-id-2> ... \
  --from <your-main-wallet> \
  --node https://test-rpc.cloud.aultblockchain.xyz \
  --chain-id ault_10904-1 \
  --gas 200000 --gas-prices 10000000aault -y
```

### Step 5: Watch Mining Start

Once licenses are delegated:

1. The miner detects the delegation (within 30 seconds)
2. VRF key is auto-registered on-chain
3. Mining starts automatically

Monitor with:

```bash
fly logs
```

Or check status:

```bash
curl https://<your-app>.fly.dev/v1/status
```

## How It Works

**Boot Sequence:**

```
Deploy -> Generate Keys -> Save to Volume -> API Healthy
       -> Status: "awaiting_delegation"

User Delegates License(s)

       -> Detect Delegation -> Register VRF Key
       -> Status: "registering_vrf"

       -> VRF Registration Success
       -> Status: "mining"
```

**Key persistence**: Keys are stored on a Fly volume at `/data/keys.json`. They persist across container restarts and redeploys.

## Mainnet Configuration

To deploy to mainnet, update environment variables:

```bash
fly secrets set \
  CHAIN_GRPC="<mainnet-grpc-endpoint>" \
  CHAIN_RPC="<mainnet-rpc-endpoint>" \
  CHAIN_ID="ault_20904-1"
```

Or modify `fly.toml` before deployment.

## Security Best Practices

1. **Your main wallet key is NEVER exposed** - only auto-generated keys are on Fly.io
2. **Use unique deployments** - each Fly app gets its own operator wallet
3. **Revoke delegation if compromised**:
   ```bash
   aultd tx miner undelegate-mining <operator-address> <license-ids...> \
     --from <main-wallet> \
     --node https://test-rpc.cloud.aultblockchain.xyz \
     --chain-id ault_10904-1 -y
   ```
4. **Monitor operator activity** via `/v1/status` endpoint

## Fly.io Configuration

The default `fly.toml` uses:

| Setting      | Value                | Purpose                     |
| ------------ | -------------------- | --------------------------- |
| Machine      | shared-cpu-1x, 256MB | Cheapest option             |
| Region       | iad (US East)        | Low latency to chain        |
| Volume       | 1GB                  | Key persistence             |
| Health check | GET /health          | Monitors chain connectivity |
| Auto-stop    | Disabled             | Mining runs 24/7            |

## Troubleshooting

### Status stuck on "awaiting_delegation"

Ensure licenses are **delegated** to the operator address. Check:

```bash
aultd q miner delegated-licenses <operator-address> \
  --node https://test-rpc.cloud.aultblockchain.xyz
```

### VRF registration keeps retrying

This is normal. VRF registration only works after a license is delegated. Once you delegate, it should succeed within 30-60 seconds.

### Health check failing

Check logs for gRPC/RPC connection errors:

```bash
fly logs
```

Verify testnet endpoints are accessible.

### Keys not persisting after restart

Ensure the volume is properly attached:

```bash
fly volumes list
```

If missing, redeploy with `fly deploy` (volumes are auto-created via `initial_size` in fly.toml).

### Deployment fails

Check Docker build:

```bash
fly deploy --verbose
```

## API Endpoints

| Endpoint                       | Description                       |
| ------------------------------ | --------------------------------- |
| `GET /health`                  | Health check (chain connectivity) |
| `GET /v1/operator`             | Operator address and status       |
| `GET /v1/status`               | Miner status and stats            |
| `GET /v1/submissions`          | List submissions                  |
| `GET /v1/rewards?license_id=X` | Query rewards for a license       |

## Cost Estimate

Fly.io pricing (as of 2024):

- **shared-cpu-1x, 256MB**: ~$2-3/month
- **1GB Volume**: ~$0.15/month
- **Bandwidth**: First 100GB free, then $0.02/GB

Mining is lightweight and should stay within free tiers for bandwidth.

## Manual Mode (Advanced)

If you prefer to provide your own keys instead of auto-generation:

```bash
# Generate keys locally
./aultmined keygen      # Save the private key
./aultmined vrfkeygen   # Save the private key

# Set as Fly secrets
fly secrets set \
  MINER_OPERATOR_KEY="<operator-private-key>" \
  MINER_VRF_KEY="<vrf-private-key>"

# Deploy (auto-mode is disabled when secrets are set)
fly deploy
```

In manual mode, you must register the VRF key yourself before mining works:

```bash
export MINER_OPERATOR_KEY="<key>"
export MINER_VRF_KEY="<key>"
./aultmined set-key
```
