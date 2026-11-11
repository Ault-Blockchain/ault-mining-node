# Miner REST API Reference

The miner client exposes a minimal REST API for health and version checks.

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
  "epoch": 42,
  "version": "v1.1.6"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `ok` | boolean | Overall health |
| `chain` | boolean | Chain gRPC/RPC reachable |
| `epoch` | number | Current epoch, if chain is reachable |
| `version` | string | Version of the running binary |

**Status Codes**

- `200` - Healthy
- `503` - Chain unavailable

## Version

Check which build is running. Useful for verifying an upgrade rolled out.

```
GET /version
```

**Response**

```json
{
  "version": "v1.1.6",
  "commit": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
  "build_date": "2026-11-11T07:41:19Z",
  "go_version": "go1.25.8",
  "platform": "linux/amd64"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Release version (git tag), `dev` for local builds |
| `commit` | string | Git commit hash, omitted if unknown |
| `build_date` | string | Build timestamp (RFC3339, UTC), omitted if unknown |
| `go_version` | string | Go toolchain used to build |
| `platform` | string | `GOOS/GOARCH` |

**Status Codes**

- `200` - Always

## Example

```bash
curl http://localhost:8080/health
curl http://localhost:8080/version
```
