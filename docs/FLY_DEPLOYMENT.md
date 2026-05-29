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

> **Note**: This guide deploys a **single replica**. For high availability with multiple
> replicas (recommended for production), see [Multi-Replica Deployment](#multi-replica-deployment-high-availability).

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

A persistent volume is automatically created on first deploy to store auto-generated keys.

The miner will:

1. Auto-generate an operator wallet and VRF key
2. Save keys to the persistent volume
3. Start healthy and wait for license delegation

### Step 3: Get Your Operator Address

```bash
fly logs
```

Look for the startup log lines:

```text
Delegate licenses to: ault1abc123...
VRF public key: deadbeef...
```

Copy the operator address for the next step.

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

Or check health:

```bash
curl https://<your-app>.fly.dev/health
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
4. **Monitor process activity** via logs and `/health`

## Fly.io Configuration

The default `fly.toml` uses:

| Setting      | Value              | Purpose                     |
| ------------ | ------------------ | --------------------------- |
| Machine      | shared-cpu-2x, 1GB | Default in fly.toml         |
| Region       | nrt (Tokyo)        | Low latency to chain        |
| Volume       | 4GB                | Key persistence             |
| Health check | GET /health        | Monitors chain connectivity |
| Auto-stop    | Disabled           | Mining runs 24/7            |

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

| Endpoint      | Description                       |
| ------------- | --------------------------------- |
| `GET /health` | Health check (chain connectivity) |

## Cost Estimate

Fly.io pricing (as of January 2026):

### Single Replica (Auto Mode)

- **shared-cpu-2x, 1GB**: ~$6.39/month
- **4GB Volume**: ~$0.60/month
- **Bandwidth**: First 100GB free, then $0.02/GB

Mining is lightweight and should stay within free tiers for bandwidth.

### Multi-Replica (Production)

See the [sizing table](#recommended-configuration) for detailed recommendations. Summary:

| Setup      | Est. Cost/mo |
| ---------- | ------------ |
| 2 replicas | ~$15-60      |
| 3 replicas | ~$60-480     |

Costs scale with license count due to increased CPU requirements for VRF computation.

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

## Multi-Replica Deployment (High Availability)

For production deployments where you cannot afford to miss epochs, deploy multiple replicas across regions.

### Why Auto Mode is Single-Replica Only

Auto mode generates keys and stores them on a Fly volume. Since each replica gets its own volume, each would generate **different keys** with different operator addresses. Licenses delegated to one operator wouldn't be mineable by other replicas.

For multi-replica deployments, all replicas must share the **same keys** via Fly secrets.

### How Multi-Replica Works

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Replica A  │     │  Replica B  │     │  Replica C  │
│  (syd)      │     │   (nrt)     │     │   (sin)     │
│  Same keys  │     │  Same keys  │     │  Same keys  │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │
                    ┌──────▼──────┐
                    │    Chain     |
                    │(deduplicates)|
                    └──────────────┘
```

Every epoch:

1. All replicas independently compute VRF proofs and solve PoW
2. All replicas race to submit their batch
3. First submission wins, others are rejected as duplicates (harmless)
4. If any replica fails, others still submit successfully

### Recommended Configuration

| Licenses    | VM Size        | Memory | Replicas | Regions       | Est. Cost/mo    |
| ----------- | -------------- | ------ | -------- | ------------- | --------------- |
| <= 10K      | shared-cpu-2x  | 512MB  | 2        | syd, nrt      | ~$3.89\*replica |
| 10K - 100K  | performance-2x | 4GB    | 2        | syd, nrt, sin | ~$62\*replica   |
| 100K - 500K | performance-4x | 8GB    | 2        | syd, nrt, sin | ~$124\*replica  |
| 500K - 1M   | performance-8x | 16GB   | 3        | syd, nrt, sin | ~$250\*replica  |

### Setup Guide

#### Step 1: Generate Keys Locally

```bash
# Download the miner binary or build from source
./aultmined keygen
# Output: Private key and operator address - SAVE THESE

./aultmined vrfkeygen
# Output: VRF private key and public key - SAVE THESE
```

#### Step 2: Store Keys as Fly Secrets

```bash
fly secrets set \
  MINER_OPERATOR_KEY="<operator-private-key-hex>" \
  MINER_VRF_KEY="<vrf-private-key-hex>"
```

#### Step 3: Configure fly.toml for Multi-Replica

Update your `fly.toml`:

```toml
[env]
  MINER_AUTO_MODE = "false"      # Disable auto mode
  MINER_BATCH_SIZE = "1000"

# Remove the [mounts] section - no volume needed with secrets
# [mounts]
#   source = "miner_data"
#   destination = "/data"
```

#### Step 4: Deploy and Scale

```bash
# Deploy the app
fly deploy

# Scale to multiple regions
fly scale count 1 --region nrt
fly scale count 1 --region syd
fly scale count 1 --region sin

```

#### Step 5: Register VRF Key On-Chain

```bash
export MINER_OPERATOR_KEY="<key>"
export MINER_VRF_KEY="<key>"
./aultmined set-key
```

#### Step 6: Delegate Licenses

Delegate your licenses to the operator address (same as single-replica setup).

### Expected Behavior

In your logs, you'll see:

- One replica successfully submits each epoch
- Other replicas show "duplicate submission" messages - **this is normal**
- If one replica goes down, others continue mining without interruption

### Monitoring

Check that at least one replica is healthy:

```bash
# Check all replicas
fly status

# Check logs across all replicas
fly logs
```

## Migrating from Auto Mode to Multi-Replica

If you started with auto mode and want to upgrade to multi-replica for reliability:

### Step 1: Extract Existing Keys

```bash
# SSH into your running auto-mode instance
fly ssh console

# Inside the container, view your keys
cat /data/keys.json
```

Output:

```json
{
  "operator_key": "abc123...",
  "vrf_key": "def456...",
  "created_at": "2025-01-01T00:00:00Z"
}
```

**Copy the `operator_key` and `vrf_key` values.**

### Step 2: Store as Fly Secrets

```bash
fly secrets set \
  MINER_OPERATOR_KEY="<operator_key from json>" \
  MINER_VRF_KEY="<vrf_key from json>"
```

### Step 3: Update fly.toml

```toml
[env]
  MINER_AUTO_MODE = "false"

# Remove or comment out the mounts section
# [mounts]
#   source = "miner_data"
#   destination = "/data"
```

### Step 4: Deploy and Scale

```bash
fly deploy

fly scale count 1 --region nrt

fly scale count 1 --region syd

fly scale count 1 --region sin

```

### What's Preserved

- **Same operator address** - no re-delegation needed
- **Same VRF key** - already registered on-chain
- **Mining continues** - no interruption during migration
