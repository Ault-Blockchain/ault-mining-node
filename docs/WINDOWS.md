# Windows Mining Guide

Run the Ault miner on Windows using pre-built binaries.

## Prerequisites

- Windows 10 or 11 (64-bit)
- Mining license(s) owned or delegated to your account

## Quick Start

### 1. Download

Download `aultmined-windows-amd64.exe` from [GitHub Releases](https://github.com/Ault-Blockchain/ault-mining-node/releases).

If Windows Defender SmartScreen blocks it: click **More info** > **Run anyway**, or right-click the file > Properties > check **Unblock**.

### 2. Choose Your Setup

**Option A - Self-mining (simplest):** Use the private key of the wallet that owns your licenses. Skip to step 3.

**Option B - Delegation (recommended for security):**

1. Generate a fresh operator wallet:
   ```powershell
   .\aultmined-windows-amd64.exe keygen
   ```
2. Save the private key and address
3. Delegate your licenses to this new address from your main wallet (required before `set-key` will work)

### 3. Generate VRF Key

```powershell
.\aultmined-windows-amd64.exe vrfkeygen
```

Save the private key.

### 4. Configure

Set environment variables in PowerShell:

```powershell
# Use your license-owning wallet's key (Option A) or the new operator key (Option B)
$env:MINER_OPERATOR_KEY = "<private-key-hex>"
$env:MINER_VRF_KEY = "<vrf-private-key-hex>"
$env:CHAIN_GRPC = "test-grpc.cloud.aultblockchain.xyz:9090"
$env:CHAIN_RPC = "https://test-rpc.cloud.aultblockchain.xyz"
$env:CHAIN_ID = "ault_10904-1"
```

### 5. Register VRF Key

```powershell
.\aultmined-windows-amd64.exe set-key
```

### 6. Start Mining

```powershell
.\aultmined-windows-amd64.exe mine --yes
```

The miner auto-detects owned and delegated licenses. Logs print to the console.
