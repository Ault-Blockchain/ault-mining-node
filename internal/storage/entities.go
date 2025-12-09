package storage

import (
	"time"
)

// Submission represents a work submission record
type Submission struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	Epoch     uint64 `gorm:"not null;index"`
	LicenseID uint64 `gorm:"not null;index;column:license_id"`
	Y         []byte `gorm:"not null"`
	Proof     []byte `gorm:"not null"`
	Nonce     []byte `gorm:"not null"`
	TxHash    *string
	CreatedAt time.Time
}

// EpochStat represents aggregated statistics for an epoch and license
type EpochStat struct {
	Epoch       uint64 `gorm:"primaryKey"`
	LicenseID   uint64 `gorm:"primaryKey;column:license_id"`
	VRFAttempts uint64 `gorm:"default:0;column:vrf_attempts"`
	Wins        uint64 `gorm:"default:0"`
	Submissions uint64 `gorm:"default:0"`
	UpdatedAt   time.Time
}

// SubmissionReward represents a submission reward record
type SubmissionReward struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	Epoch     uint64 `gorm:"not null;uniqueIndex:idx_reward_epoch_license,priority:1"`
	LicenseID uint64 `gorm:"not null;index;uniqueIndex:idx_reward_epoch_license,priority:2;column:license_id"`
	Credits   uint64 `gorm:"not null;default:1"`
	Payout    *string
	SettledAt *time.Time `gorm:"column:settled_at"`
	CreatedAt time.Time
}

// MinerSession stores persistent mining session info
type MinerSession struct {
	ID                 uint64 `gorm:"primaryKey;autoIncrement"`
	OwnerAddress       string `gorm:"not null;uniqueIndex"`
	StartEpoch         uint64 `gorm:"not null;column:start_epoch"`
	LastProcessedEpoch uint64 `gorm:"not null;column:last_processed_epoch"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
