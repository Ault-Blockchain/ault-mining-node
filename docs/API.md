# Miner REST API Reference

The miner client exposes a REST API for monitoring status, querying submissions, and checking rewards.

## Base URL

Default: `http://localhost:8080`

Configure via `MINER_API_PORT` environment variable.

## Endpoints

### Health Check

Check database and chain connectivity.

```
GET /health
```

**Response**

```json
{
  "ok": true,
  "db": true,
  "chain": true,
  "epoch": 42
}
```

| Field | Type | Description |
|-------|------|-------------|
| `ok` | boolean | Overall health (db AND chain) |
| `db` | boolean | SQLite database accessible |
| `chain` | boolean | Chain gRPC/RPC reachable |
| `epoch` | number | Current epoch (if chain is reachable) |

**Status Codes**
- `200` - Healthy
- `503` - Unhealthy (db or chain unavailable)

---

### Miner Status

Get current miner status and mining statistics.

```
GET /v1/status
```

**Response**

```json
{
  "running": true,
  "stats": {
    "start_time": "2024-01-15T10:30:00Z",
    "start_epoch": 100,
    "last_processed_epoch": 142,
    "total_attempts": 4200,
    "total_wins": 15,
    "total_submissions": 15,
    "license_stats": {
      "1": {
        "license_id": 1,
        "vrf_attempts": 2100,
        "wins": 8,
        "submissions": 8,
        "last_win_epoch": 140,
        "last_win_time": "2024-01-15T11:20:00Z",
        "current_epoch": 142
      },
      "2": {
        "license_id": 2,
        "vrf_attempts": 2100,
        "wins": 7,
        "submissions": 7,
        "last_win_epoch": 138,
        "last_win_time": "2024-01-15T11:10:00Z",
        "current_epoch": 142
      }
    }
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `running` | boolean | Miner is active |
| `stats.start_time` | string | Mining session start time (RFC3339) |
| `stats.start_epoch` | number | First epoch processed |
| `stats.last_processed_epoch` | number | Most recent epoch processed |
| `stats.total_attempts` | number | Total VRF computations across all licenses |
| `stats.total_wins` | number | Total winning VRF outputs |
| `stats.total_submissions` | number | Total submissions sent to chain |
| `stats.license_stats` | object | Per-license statistics (keyed by license ID) |

---

### List Submissions

Query submission history from local database.

```
GET /v1/submissions
```

**Query Parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `license_id` | number | - | Filter by license ID |
| `epoch` | number | - | Filter by epoch |
| `limit` | number | 100 | Max results to return |
| `offset` | number | 0 | Skip first N results |

**Response**

```json
[
  {
    "id": 1,
    "epoch": 140,
    "license_id": 1,
    "y": "a1b2c3...",
    "proof": "d4e5f6...",
    "nonce": "789abc...",
    "tx_hash": "ABCD1234...",
    "created_at": "2024-01-15T11:20:00Z"
  }
]
```

| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Database record ID |
| `epoch` | number | Mining epoch |
| `license_id` | number | License used for mining |
| `y` | string | VRF output (hex) |
| `proof` | string | VRF proof (hex) |
| `nonce` | string | PoW nonce (hex) |
| `tx_hash` | string | Transaction hash (if submitted) |
| `created_at` | string | Record creation time |

---

### Query Rewards

Get rewards earned by this miner for a specific license.

```
GET /v1/rewards
```

**Query Parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `license_id` | number | **required** | License ID to query |
| `source` | string | `chain` | Data source: `chain` (gRPC) or `db` (local) |

**Response**

```json
{
  "license_id": 1,
  "total_payout": "1000000000000000000",
  "total_credits": 10,
  "from_epoch": 100,
  "to_epoch": 142,
  "source": "chain"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `license_id` | number | Queried license ID |
| `total_payout` | string | Total AULT earned (in aault, smallest unit) |
| `total_credits` | number | Total mining credits accumulated |
| `from_epoch` | number | Epoch when this miner started |
| `to_epoch` | number | Last processed epoch by this miner |
| `source` | string | Data source used (`chain` or `db`) |

**Notes**

- Returns rewards from `start_epoch` (when miner started) to `last_processed_epoch`
- `source=chain`: Queries on-chain state via gRPC (authoritative)
- `source=db`: Queries local database (settled rewards only)

**Error Codes**

- `400` - Missing or invalid `license_id`
- `500` - Query failed
- `503` - Chain client unavailable (for `source=chain`)

---

## Example Usage

```bash
# Health check
curl http://localhost:8080/health

# Get miner status
curl http://localhost:8080/v1/status

# List recent submissions
curl http://localhost:8080/v1/submissions?limit=10

# List submissions for specific license
curl "http://localhost:8080/v1/submissions?license_id=1&limit=50"

# Query rewards from chain
curl "http://localhost:8080/v1/rewards?license_id=1"

# Query rewards from local DB
curl "http://localhost:8080/v1/rewards?license_id=1&source=db"
```
