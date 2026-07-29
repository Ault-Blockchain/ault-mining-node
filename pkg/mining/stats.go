// Package mining provides core mining functionality for the Ault blockchain.
// This file contains statistics tracking and metrics.
package mining

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricVRFAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{Name: "miner_vrf_attempts_total", Help: "Total VRF attempts per license"},
		[]string{"license_id"},
	)
	metricWins = promauto.NewCounterVec(
		prometheus.CounterOpts{Name: "miner_wins_total", Help: "Total VRF wins per license"},
		[]string{"license_id"},
	)
	metricSubmissions = promauto.NewCounterVec(
		prometheus.CounterOpts{Name: "miner_submissions_total", Help: "Total submitted works per license"},
		[]string{"license_id"},
	)
	metricSubmitFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miner_submit_failures_total",
			Help: "Failed BatchSubmitWork submissions, classified by reason.",
		},
		[]string{"reason"},
	)
	metricVRFProofFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miner_vrf_proof_failures_total",
			Help: "VRF proof generation failures per license.",
		},
		[]string{"license_id"},
	)
	metricPoWFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miner_pow_failures_total",
			Help: "PoW solving failures per license (max attempts exceeded).",
		},
		[]string{"license_id"},
	)
	metricOperatorInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miner_operator_info",
			Help: "Info gauge (value=1) exposing this miner's operator address as a label. Use for label joins.",
		},
		[]string{"operator"},
	)
)

var (
	metricVRFDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "miner_vrf_duration_seconds",
		Help:    "VRF proof generation duration",
		Buckets: prometheus.ExponentialBuckets(0.0005, 2, 13),
	})
	metricPoWDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "miner_pow_duration_seconds",
		Help:    "PoW solving duration",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14),
	})
)

// MiningStats tracks overall mining statistics (thread-safe via atomics)
type MiningStats struct {
	StartTime          time.Time                `json:"start_time"`
	StartEpoch         uint64                   `json:"start_epoch"`          // Epoch when mining started
	LastProcessedEpoch uint64                   `json:"last_processed_epoch"` // Use atomic.StoreUint64 / atomic.LoadUint64
	TotalAttempts      uint64                   `json:"total_attempts"`       // Use atomic.AddUint64 / atomic.LoadUint64
	TotalWins          uint64                   `json:"total_wins"`           // Use atomic.AddUint64 / atomic.LoadUint64
	TotalSubmissions   uint64                   `json:"total_submissions"`    // Use atomic.AddUint64 / atomic.LoadUint64
	LicenseStats       map[uint64]*LicenseStats `json:"license_stats"`        // Read-only after initialization
}

// LicenseStats tracks mining statistics for a single license (thread-safe via atomics)
type LicenseStats struct {
	LicenseID    uint64    `json:"license_id"`
	VrfAttempts  uint64    `json:"vrf_attempts"`   // Use atomic.AddUint64 / atomic.LoadUint64
	Wins         uint64    `json:"wins"`           // Use atomic.AddUint64 / atomic.LoadUint64
	Submissions  uint64    `json:"submissions"`    // Use atomic.AddUint64 / atomic.LoadUint64
	LastWinEpoch uint64    `json:"last_win_epoch"` // Use atomic.StoreUint64 / atomic.LoadUint64
	LastWinTime  time.Time `json:"last_win_time"`  // Not atomic, but only written on wins (rare)
	CurrentEpoch uint64    `json:"current_epoch"`  // Use atomic.StoreUint64 / atomic.LoadUint64
}

// GetStats returns current mining statistics
func (m *MinerManager) GetStats() MiningStats {
	// Create a copy with atomic loads
	stats := MiningStats{
		StartTime:          m.stats.StartTime,
		StartEpoch:         m.stats.StartEpoch,
		LastProcessedEpoch: atomic.LoadUint64(&m.stats.LastProcessedEpoch),
		TotalAttempts:      atomic.LoadUint64(&m.stats.TotalAttempts),
		TotalWins:          atomic.LoadUint64(&m.stats.TotalWins),
		TotalSubmissions:   atomic.LoadUint64(&m.stats.TotalSubmissions),
		LicenseStats:       make(map[uint64]*LicenseStats),
	}

	// Copy license stats (map is read-only, stats use atomics)
	for id, ls := range m.stats.LicenseStats {
		stats.LicenseStats[id] = &LicenseStats{
			LicenseID:    ls.LicenseID,
			VrfAttempts:  atomic.LoadUint64(&ls.VrfAttempts),
			Wins:         atomic.LoadUint64(&ls.Wins),
			Submissions:  atomic.LoadUint64(&ls.Submissions),
			LastWinEpoch: atomic.LoadUint64(&ls.LastWinEpoch),
			LastWinTime:  ls.LastWinTime, // Rare write, acceptable race
			CurrentEpoch: atomic.LoadUint64(&ls.CurrentEpoch),
		}
	}

	return stats
}

// GetStartTime returns the start time of the mining session
func (s *MiningStats) GetStartTime() time.Time {
	return s.StartTime
}

// GetLicenseStats returns the license statistics map
func (s *MiningStats) GetLicenseStats() map[uint64]*LicenseStats {
	return s.LicenseStats
}

// GetLicenseID returns the license ID
func (ls *LicenseStats) GetLicenseID() uint64 {
	return ls.LicenseID
}

// GetVRFAttemptsPtr returns a pointer to the VrfAttempts field for atomic operations
func (ls *LicenseStats) GetVRFAttemptsPtr() *uint64 {
	return &ls.VrfAttempts
}

// GetWinsPtr returns a pointer to the Wins field for atomic operations
func (ls *LicenseStats) GetWinsPtr() *uint64 {
	return &ls.Wins
}

// GetSubmissionsPtr returns a pointer to the Submissions field for atomic operations
func (ls *LicenseStats) GetSubmissionsPtr() *uint64 {
	return &ls.Submissions
}
