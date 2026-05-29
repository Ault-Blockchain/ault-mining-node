# Miner REST API Reference

The miner client exposes a minimal REST API for health checks.

## Base URL

Default: `http://localhost:8080`

Configure via `MINER_API_PORT` environment variable.

## Health Check

Check chain connectivity.

```
GET /health
```

**Response**

```json
{
  "ok": true,
  "chain": true,
  "epoch": 42
}
```

| Field | Type | Description |
|-------|------|-------------|
| `ok` | boolean | Overall health |
| `chain` | boolean | Chain gRPC/RPC reachable |
| `epoch` | number | Current epoch, if chain is reachable |

**Status Codes**

- `200` - Healthy
- `503` - Chain unavailable

## Example

```bash
curl http://localhost:8080/health
```
