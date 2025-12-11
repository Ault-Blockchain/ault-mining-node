// Package mining provides core mining functionality for the Ault blockchain.
// This file contains the core mining orchestration and manager logic.
package mining

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProtonMail/go-ecvrf/ecvrf"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	stor "github.com/Ault-Blockchain/ault-miner-node/internal/storage"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

// NewMinerManager creates a new miner manager for the owner
func NewMinerManager(chainClient ChainClient) (*MinerManager, error) {
	// Load VRF key from environment variable
	vrfPrivKey, vrfPubKey, err := loadVRFKeyFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to load VRF key: %w", err)
	}

	// Get owner address from chain client (derived from MINER_OPERATOR_KEY)
	ownerAddr, err := chainClient.GetOwnerAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get owner address: %w", err)
	}
	log.Printf("Owner address: %s", ownerAddr.String())

	// Always enable storage with default path
	const storagePath = "data/miner.db"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := stor.Open(ctx, stor.Options{
		Path:        storagePath,
		WAL:         true,
		BusyTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Printf("Warning: failed to open storage: %v", err)
	} else {
		log.Printf("SQLite storage opened at %s", storagePath)
	}

	// Auto-detect licenses from chain (both owned and delegated)
	log.Printf("Auto-detecting licenses from chain...")

	// Get owned licenses first (for self-mining)
	var licenses []uint64
	ownedLicenses, err := chainClient.GetOwnedLicenses(context.Background(), ownerAddr.String())
	if err != nil {
		log.Printf("Warning: Failed to query owned licenses: %v", err)
	} else if len(ownedLicenses) > 0 {
		log.Printf("Auto-detected %d owned license(s)", len(ownedLicenses))
		licenses = append(licenses, ownedLicenses...)
	}

	// Get delegated licenses (for operator mode)
	delegatedLicenses, err := chainClient.GetDelegatedLicenses(context.Background(), ownerAddr.String())
	if err != nil {
		log.Printf("Warning: Failed to query delegated licenses: %v", err)
	} else if len(delegatedLicenses) > 0 {
		log.Printf("Auto-detected %d delegated license(s)", len(delegatedLicenses))
		// Add delegated licenses (no duplicates since self-delegation is not allowed)
		licenses = append(licenses, delegatedLicenses...)
	}

	if len(licenses) == 0 {
		log.Printf("No licenses found for %s", ownerAddr.String())
	} else {
		log.Printf("Total licenses to mine: %d", len(licenses))
	}

	// Initialize statistics
	stats := &MiningStats{
		StartTime:    time.Now(),
		LicenseStats: make(map[uint64]*LicenseStats),
	}
	for _, licenseID := range licenses {
		stats.LicenseStats[licenseID] = &LicenseStats{
			LicenseID: licenseID,
		}
	}

	m := &MinerManager{
		ownerAddr:   ownerAddr,
		vrfPrivKey:  vrfPrivKey,
		vrfPubKey:   vrfPubKey,
		licenses:    licenses,
		chainClient: chainClient,
		stats:       stats,
		store:       db,
	}

	// Optional sanity: compare configured owner VRF key with chain state when present
	if keyInfo, err := chainClient.GetOwnerKeyInfo(context.Background(), ownerAddr.String()); err == nil && keyInfo != nil && len(keyInfo.VrfPubkey) == 32 {
		if !bytes.Equal(keyInfo.VrfPubkey, vrfPubKey) {
			log.Printf("Warning: local VRF pubkey differs from on-chain owner key (nonce %d)", keyInfo.Nonce)
		}
	}

	// For each license, if miner info has a VRF pubkey, ensure it matches local owner key
	for _, lid := range licenses {
		if info, err := chainClient.GetLicenseMinerInfo(context.Background(), lid); err == nil && len(info.VrfPubkey) == 32 {
			if !bytes.Equal(info.VrfPubkey, vrfPubKey) {
				log.Printf("Warning: license %d VRF key differs from local owner key; ensure correct owner is configured", lid)
			}
		}
	}

	return m, nil
}

// loadVRFKeyFromEnv loads VRF key from MINER_VRF_KEY environment variable
func loadVRFKeyFromEnv() ([]byte, []byte, error) {
	hexKey := config.Get().VRFKey
	if hexKey == "" {
		return nil, nil, fmt.Errorf("MINER_VRF_KEY environment variable is required")
	}

	privKeyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hex VRF key: %w", err)
	}

	pk, err := ecvrf.NewPrivateKey(privKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid ECVRF private key: %w", err)
	}

	pubKey, err := pk.Public()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to derive public key: %w", err)
	}

	pubKeyBytes := pubKey.Bytes()
	if len(pubKeyBytes) != 32 {
		return nil, nil, fmt.Errorf("invalid VRF public key size: expected 32 bytes, got %d", len(pubKeyBytes))
	}

	log.Println("Loaded VRF key from MINER_VRF_KEY")
	return pk.Bytes(), pubKeyBytes, nil
}

// Start begins mining with all configured licenses
func (m *MinerManager) Start(ctx context.Context) error {
	log.Printf("Starting miner for owner %s with %d licenses", m.ownerAddr.String(), len(m.licenses))

	// Start monitoring epochs
	epochChan := make(chan *minertypes.QueryEpochResponse, 1)
	go m.chainClient.MonitorEpochs(ctx, epochChan)

	// Main mining loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case epochInfo := <-epochChan:
			m.processEpoch(ctx, epochInfo)
		}
	}
}

// loadOrInitSession loads existing session from DB or creates a new one
func (m *MinerManager) loadOrInitSession(ctx context.Context, currentEpoch uint64) {
	if m.store == nil {
		// No DB, just use current epoch
		m.stats.StartEpoch = currentEpoch
		atomic.StoreUint64(&m.stats.LastProcessedEpoch, 0)
		log.Printf("Mining session started at epoch %d (no DB)", currentEpoch)
		return
	}

	// Try to load existing session
	session, err := m.store.GetMinerSession(ctx, m.ownerAddr.String())
	if err == nil && session != nil {
		// Existing session found
		m.stats.StartEpoch = session.StartEpoch
		atomic.StoreUint64(&m.stats.LastProcessedEpoch, session.LastProcessedEpoch)
		log.Printf("Resumed mining session from epoch %d (last processed: %d)", session.StartEpoch, session.LastProcessedEpoch)
		return
	}

	// No existing session, create new one
	m.stats.StartEpoch = currentEpoch
	atomic.StoreUint64(&m.stats.LastProcessedEpoch, 0)
	if err := m.store.CreateOrUpdateSession(ctx, m.ownerAddr.String(), currentEpoch, currentEpoch); err != nil {
		log.Printf("Warning: failed to save session to DB: %v", err)
	}
	log.Printf("Mining session started at epoch %d", currentEpoch)
}

// processEpoch processes a new epoch for all licenses
func (m *MinerManager) processEpoch(ctx context.Context, epochInfo *minertypes.QueryEpochResponse) {
	// Load or initialize start epoch from DB
	if m.stats.StartEpoch == 0 {
		m.loadOrInitSession(ctx, epochInfo.Epoch)
	}

	// Log epoch processing
	log.Printf("Processing epoch %d (seed: %x, threshold: %x)",
		epochInfo.Epoch, epochInfo.Seed[:8], epochInfo.Threshold[:8])

	// Process each license in parallel and collect results
	resultsChan := make(chan *minertypes.WorkSubmission, len(m.licenses))
	var wg sync.WaitGroup
	for _, licenseID := range m.licenses {
		wg.Add(1)
		go func(lid uint64) {
			defer wg.Done()
			if result := m.processLicenseForBatch(ctx, lid, epochInfo); result != nil {
				select {
				case resultsChan <- result:
				case <-ctx.Done():
				}
			}
		}(licenseID)
	}

	// Close channel once all workers are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect all work results
	var workResults []minertypes.WorkSubmission
	for result := range resultsChan {
		workResults = append(workResults, *result)
	}

	// If we have any wins, submit them (respecting batch size config)
	if len(workResults) > 0 {
		batchSize := config.Get().BatchSize
		if batchSize <= 0 || batchSize >= len(workResults) {
			// Submit all at once
			log.Printf("🎯 Total wins: %d licenses. Submitting batch...", len(workResults))
			m.submitBatchWork(ctx, workResults)
		} else {
			// Split into batches
			log.Printf("🎯 Total wins: %d licenses. Submitting in batches of %d...", len(workResults), batchSize)
			for i := 0; i < len(workResults); i += batchSize {
				end := i + batchSize
				if end > len(workResults) {
					end = len(workResults)
				}
				batch := workResults[i:end]
				log.Printf("📦 Submitting batch %d/%d (%d submissions)...", (i/batchSize)+1, (len(workResults)+batchSize-1)/batchSize, len(batch))
				m.submitBatchWork(ctx, batch)
			}
		}
	}

	// Update last processed epoch (in-memory and DB)
	atomic.StoreUint64(&m.stats.LastProcessedEpoch, epochInfo.Epoch)
	if m.store != nil {
		_ = m.store.UpdateLastProcessedEpoch(ctx, m.ownerAddr.String(), epochInfo.Epoch)
	}
}

// processLicenseForBatch processes mining for a single license and returns result if won
func (m *MinerManager) processLicenseForBatch(ctx context.Context, licenseID uint64, epochInfo *minertypes.QueryEpochResponse) *minertypes.WorkSubmission {
	log.Printf("License %d: Processing for epoch %d", licenseID, epochInfo.Epoch)

	// Pre-check: Query license eligibility before doing any work
	licenseInfo, err := m.chainClient.GetLicenseMinerInfo(ctx, licenseID)
	if err != nil {
		log.Printf("License %d: Failed to query info: %v", licenseID, err)
		// Continue anyway - we'll try VRF and let chain validate
	} else {
		vrfKeyPreview := "empty"
		if len(licenseInfo.VrfPubkey) >= 8 {
			vrfKeyPreview = fmt.Sprintf("%x", licenseInfo.VrfPubkey[:8])
		}
		log.Printf("License %d: EligibleNow=%v, LastSubmitEpoch=%d, VRFPubkey=%s",
			licenseID, licenseInfo.EligibleNow, licenseInfo.LastSubmitEpoch, vrfKeyPreview)
		// Check if license is eligible
		if !licenseInfo.EligibleNow {
			log.Printf("License %d: Not eligible (eligible_now=false)", licenseID)
			return nil
		}
		// Check rate limit: already submitted this epoch
		if licenseInfo.LastSubmitEpoch == epochInfo.Epoch {
			log.Printf("License %d: Already submitted in epoch %d", licenseID, epochInfo.Epoch)
			return nil
		}
	}

	// Get stats for this license (map is read-only after init, so safe)
	stats := m.stats.LicenseStats[licenseID]
	if stats != nil {
		atomic.AddUint64(&stats.VrfAttempts, 1)
		atomic.StoreUint64(&stats.CurrentEpoch, epochInfo.Epoch)
	}
	atomic.AddUint64(&m.stats.TotalAttempts, 1)
	if m.store != nil {
		_ = m.store.IncrementAttempts(ctx, epochInfo.Epoch, licenseID)
	}
	metricVRFAttempts.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()

	// Build VRF input message
	vrfMsg := BuildVRFMessage(epochInfo.Seed, licenseID, m.ownerAddr)

	// Generate VRF proof
	vrfStart := time.Now()
	y, proof, err := GenerateVRFProof(m.vrfPrivKey, vrfMsg)
	if err != nil {
		log.Printf("License %d: VRF proof generation failed: %v", licenseID, err)
		log.Printf("💡 This may indicate a corrupted VRF key. Try regenerating with: ./aultmined keygen")
		return nil
	}
	metricVRFDuration.Observe(time.Since(vrfStart).Seconds())

	// Check if we're below threshold
	if !IsBelowThreshold(y, epochInfo.Threshold) {
		return nil
	}

	// Update win stats (using atomics)
	if stats != nil {
		atomic.AddUint64(&stats.Wins, 1)
		atomic.StoreUint64(&stats.LastWinEpoch, epochInfo.Epoch)
		stats.LastWinTime = time.Now() // Rare write, acceptable race
	}
	atomic.AddUint64(&m.stats.TotalWins, 1)
	if m.store != nil {
		_ = m.store.IncrementWin(ctx, epochInfo.Epoch, licenseID)
	}
	metricWins.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()
	log.Printf("🎯 License %d: VRF WIN! Starting PoW...", licenseID)

	// Solve proof-of-work with fixed difficulty (k=16 leading zero bits)
	kFixed := uint32(16)
	powStart := time.Now()
	nonce, err := SolvePoW(epochInfo.Seed, licenseID, y, kFixed)
	if err != nil {
		log.Printf("License %d: PoW failed after max attempts: %v", licenseID, err)
		log.Printf("💡 This is unusual - PoW should usually succeed. Check CPU availability")
		return nil
	}
	metricPoWDuration.Observe(time.Since(powStart).Seconds())

	log.Printf("License %d: PoW solved! Nonce: %x", licenseID, nonce)

	// Return work result for batch submission
	return &minertypes.WorkSubmission{
		LicenseId: licenseID,
		Epoch:     epochInfo.Epoch,
		Y:         y,
		Proof:     proof,
		Nonce:     nonce,
	}
}

// Getter methods for MinerManager fields

// GetChainClient returns the chain client
func (m *MinerManager) GetChainClient() ChainClient {
	return m.chainClient
}

// GetOwnerAddr returns the owner address
func (m *MinerManager) GetOwnerAddr() sdk.AccAddress {
	return m.ownerAddr
}

// GetVRFPubKey returns the VRF public key
func (m *MinerManager) GetVRFPubKey() []byte {
	return m.vrfPubKey
}

// GetLicenses returns the list of licenses
func (m *MinerManager) GetLicenses() []uint64 {
	return m.licenses
}

// GetStore returns the storage database
func (m *MinerManager) GetStore() *stor.DB {
	return m.store
}

// SetStore sets the storage database
func (m *MinerManager) SetStore(db *stor.DB) {
	m.store = db
}

// ProcessEpoch is a public wrapper for processEpoch
func (m *MinerManager) ProcessEpoch(ctx context.Context, epochInfo *minertypes.QueryEpochResponse) {
	m.processEpoch(ctx, epochInfo)
}

// submitBatchWork submits multiple mining work results to the chain in a single transaction
func (m *MinerManager) submitBatchWork(ctx context.Context, workResults []minertypes.WorkSubmission) {
	if len(workResults) == 0 {
		return
	}

	// Validate all results are from the same epoch (should always be true given our collection logic)
	expectedEpoch := workResults[0].Epoch
	for i, result := range workResults {
		if result.Epoch != expectedEpoch {
			log.Printf("⚠️  Warning: Mixed epochs in batch (result %d has epoch %d, expected %d)", i, result.Epoch, expectedEpoch)
		}
	}

	// Submit batch work
	txHash, err := m.chainClient.BatchSubmitWork(ctx, workResults)
	if err != nil {
		log.Printf("❌ Failed to submit batch work: %v", err)
		if strings.Contains(err.Error(), "duplicate") {
			log.Printf("💡 Some licenses may have already submitted for this epoch")
		} else if strings.Contains(err.Error(), "not eligible") {
			log.Printf("💡 Some licenses may be quarantined or not properly configured")
		} else if strings.Contains(err.Error(), "invalid proof") {
			log.Printf("💡 VRF key mismatch - ensure your key is registered on-chain")
		} else if strings.Contains(err.Error(), "VRF verification failed") {
			log.Printf("🔍 Debug: VRF verification failed for some proofs")
		} else if strings.Contains(err.Error(), "key too young") || strings.Contains(err.Error(), "key registered at epoch") {
			log.Printf("⏱️  VRF key is too young - must wait a few epochs after registration before mining")
		} else if strings.Contains(err.Error(), "confirmation failed") {
			// Transaction was submitted but not confirmed - still count as submission
			for _, result := range workResults {
				if stats := m.stats.LicenseStats[result.LicenseId]; stats != nil {
					atomic.AddUint64(&stats.Submissions, 1)
				}
			}
			atomic.AddUint64(&m.stats.TotalSubmissions, uint64(len(workResults)))
			log.Printf("💡 Transaction submitted but confirmation timed out - check tx status manually")
		} else {
			log.Printf("💡 Check chain connection and account balance for gas")
		}
		return
	}

	// Update submission stats (using atomics)
	for _, result := range workResults {
		if stats := m.stats.LicenseStats[result.LicenseId]; stats != nil {
			atomic.AddUint64(&stats.Submissions, 1)
		}
	}
	atomic.AddUint64(&m.stats.TotalSubmissions, uint64(len(workResults)))
	log.Printf("✅ Successfully submitted batch work for %d licenses in epoch %d", len(workResults), workResults[0].Epoch)
	if m.store != nil {
		items := make([]stor.SubmissionItem, 0, len(workResults))
		for _, r := range workResults {
			items = append(items, stor.SubmissionItem{
				Epoch:     r.Epoch,
				LicenseID: r.LicenseId,
				Y:         r.Y,
				Proof:     r.Proof,
				Nonce:     r.Nonce,
			})
		}
		_ = m.store.RecordBatchSubmission(ctx, workResults[0].Epoch, items, txHash)
		// Record submission rewards for settlement tracking
		for _, r := range workResults {
			_ = m.store.RecordSubmissionReward(ctx, r.Epoch, r.LicenseId, 1)
		}
	}
	for _, r := range workResults {
		metricSubmissions.WithLabelValues(strconv.FormatUint(r.LicenseId, 10)).Inc()
	}
}
