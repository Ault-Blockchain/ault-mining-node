package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	gorm *gorm.DB
}

// Options configures database opening
type Options struct {
	Path        string
	WAL         bool
	BusyTimeout time.Duration
	Debug       bool
}

// Open opens a SQLite database with the given options
func Open(ctx context.Context, opts Options) (*DB, error) {
	if opts.Path == "" {
		opts.Path = "data/miner.db"
	}
	if opts.BusyTimeout == 0 {
		opts.BusyTimeout = 5 * time.Second
	}

	// Ensure directory exists
	if dir := filepath.Dir(opts.Path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}

	// Build DSN with pragmas
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(ON)&_pragma=busy_timeout(%d)", opts.Path, int(opts.BusyTimeout.Milliseconds()))
	if opts.WAL {
		dsn += "&_pragma=journal_mode(WAL)"
	}

	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}
	if opts.Debug {
		config.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(sqlite.Open(dsn), config)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Auto-migrate all models
	if err := db.AutoMigrate(
		&Submission{},
		&EpochStat{},
		&SubmissionReward{},
		&MinerSession{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return &DB{gorm: db}, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	sqlDB, err := d.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// EpochStatsByEpoch returns statistics for a specific epoch
func (d *DB) EpochStatsByEpoch(ctx context.Context, epoch uint64) ([]EpochStat, error) {
	var stats []EpochStat
	err := d.gorm.WithContext(ctx).
		Where("epoch = ?", epoch).
		Order("license_id ASC").
		Find(&stats).Error
	return stats, err
}

// RecordBatchSubmission records multiple submissions in a transaction
func (d *DB) RecordBatchSubmission(ctx context.Context, epoch uint64, items []SubmissionItem, txHash string) error {
	return d.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()

		for _, item := range items {
			// Insert submission
			sub := Submission{
				Epoch:     epoch,
				LicenseID: item.LicenseID,
				Y:         item.Y,
				Proof:     item.Proof,
				Nonce:     item.Nonce,
				CreatedAt: now,
			}
			if txHash != "" {
				sub.TxHash = &txHash
			}
			if err := tx.Create(&sub).Error; err != nil {
				return err
			}

			// Upsert epoch_stats
			stat := EpochStat{
				Epoch:       epoch,
				LicenseID:   item.LicenseID,
				VRFAttempts: 0,
				Wins:        0,
				Submissions: 1,
				UpdatedAt:   now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "epoch"}, {Name: "license_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"submissions": gorm.Expr("submissions + 1"),
					"updated_at":  now,
				}),
			}).Create(&stat).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// IncrementAttempts increments the VRF attempt counter
func (d *DB) IncrementAttempts(ctx context.Context, epoch, licenseID uint64) error {
	now := time.Now().UTC()
	stat := EpochStat{
		Epoch:       epoch,
		LicenseID:   licenseID,
		VRFAttempts: 1,
		Wins:        0,
		Submissions: 0,
		UpdatedAt:   now,
	}
	return d.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "epoch"}, {Name: "license_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"vrf_attempts": gorm.Expr("vrf_attempts + 1"),
			"updated_at":   now,
		}),
	}).Create(&stat).Error
}

// IncrementWin increments the win counter
func (d *DB) IncrementWin(ctx context.Context, epoch, licenseID uint64) error {
	now := time.Now().UTC()
	stat := EpochStat{
		Epoch:       epoch,
		LicenseID:   licenseID,
		VRFAttempts: 0,
		Wins:        1,
		Submissions: 0,
		UpdatedAt:   now,
	}
	return d.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "epoch"}, {Name: "license_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"wins":       gorm.Expr("wins + 1"),
			"updated_at": now,
		}),
	}).Create(&stat).Error
}

// RecentSubmissions returns the most recent submissions
func (d *DB) RecentSubmissions(ctx context.Context, limit int) ([]Submission, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var submissions []Submission
	err := d.gorm.WithContext(ctx).
		Order("id DESC").
		Limit(limit).
		Find(&submissions).Error
	return submissions, err
}

// ListSubmissions returns submissions matching the filter
func (d *DB) ListSubmissions(ctx context.Context, f SubmissionFilter) ([]Submission, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	query := d.gorm.WithContext(ctx).Model(&Submission{})

	if f.LicenseID != nil {
		query = query.Where("license_id = ?", *f.LicenseID)
	}
	if f.Epoch != nil {
		query = query.Where("epoch = ?", *f.Epoch)
	}

	var submissions []Submission
	err := query.
		Order("id DESC").
		Limit(limit).
		Offset(f.Offset).
		Find(&submissions).Error
	return submissions, err
}

// RecordSubmissionReward records a successful submission reward
func (d *DB) RecordSubmissionReward(ctx context.Context, epoch, licenseID, credits uint64) error {
	record := SubmissionReward{
		Epoch:     epoch,
		LicenseID: licenseID,
		Credits:   credits,
		CreatedAt: time.Now().UTC(),
	}
	return d.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "epoch"}, {Name: "license_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"credits"}),
	}).Create(&record).Error
}

// UpdateSubmissionPayout updates the payout for a submission
func (d *DB) UpdateSubmissionPayout(ctx context.Context, epoch, licenseID uint64, payout string) error {
	now := time.Now().UTC()
	return d.gorm.WithContext(ctx).
		Model(&SubmissionReward{}).
		Where("epoch = ? AND license_id = ?", epoch, licenseID).
		Updates(map[string]interface{}{
			"payout":     payout,
			"settled_at": now,
		}).Error
}

// GetUnsettledSubmissions returns submissions that haven't been settled yet
func (d *DB) GetUnsettledSubmissions(ctx context.Context, licenseID uint64) ([]SubmissionReward, error) {
	var rewards []SubmissionReward
	err := d.gorm.WithContext(ctx).
		Where("license_id = ? AND settled_at IS NULL", licenseID).
		Order("epoch ASC").
		Find(&rewards).Error
	return rewards, err
}

// GetTotalEarnedRewards returns total settled payout for a license
func (d *DB) GetTotalEarnedRewards(ctx context.Context, licenseID uint64) (string, uint64, error) {
	type result struct {
		TotalPayout  *int64
		TotalCredits uint64
	}
	var r result
	err := d.gorm.WithContext(ctx).
		Model(&SubmissionReward{}).
		Select("COALESCE(SUM(CAST(payout AS INTEGER)), 0) as total_payout, COALESCE(SUM(credits), 0) as total_credits").
		Where("license_id = ? AND settled_at IS NOT NULL", licenseID).
		Scan(&r).Error
	if err != nil {
		return "0", 0, err
	}

	payoutStr := "0"
	if r.TotalPayout != nil {
		payoutStr = fmt.Sprintf("%d", *r.TotalPayout)
	}
	return payoutStr, r.TotalCredits, nil
}

// GetMinerSession returns the miner session for the given owner address
func (d *DB) GetMinerSession(ctx context.Context, ownerAddress string) (*MinerSession, error) {
	var session MinerSession
	err := d.gorm.WithContext(ctx).
		Where("owner_address = ?", ownerAddress).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// CreateOrUpdateSession creates or updates a miner session
func (d *DB) CreateOrUpdateSession(ctx context.Context, ownerAddress string, startEpoch, lastProcessedEpoch uint64) error {
	now := time.Now().UTC()
	session := MinerSession{
		OwnerAddress:       ownerAddress,
		StartEpoch:         startEpoch,
		LastProcessedEpoch: lastProcessedEpoch,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	return d.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_address"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_processed_epoch": lastProcessedEpoch,
			"updated_at":           now,
		}),
	}).Create(&session).Error
}

// UpdateLastProcessedEpoch updates the last processed epoch for the owner
func (d *DB) UpdateLastProcessedEpoch(ctx context.Context, ownerAddress string, epoch uint64) error {
	return d.gorm.WithContext(ctx).
		Model(&MinerSession{}).
		Where("owner_address = ?", ownerAddress).
		Updates(map[string]interface{}{
			"last_processed_epoch": epoch,
			"updated_at":           time.Now().UTC(),
		}).Error
}
