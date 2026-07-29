// Package mining provides core mining functionality for the Ault blockchain.
// This file contains the core mining orchestration and manager logic.
package mining

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProtonMail/go-ecvrf/ecvrf"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	minertypes "github.com/Ault-Blockchain/ault/v2/x/miner/types"
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

	// Auto-detect licenses from chain (both owned and delegated) in parallel
	log.Printf("Auto-detecting licenses from chain...")

	var licenses []uint64
	var ownedLicenses, delegatedLicenses []uint64
	var ownedErr, delegatedErr error
	var wg sync.WaitGroup

	// Query owned and delegated licenses
	wg.Add(2)
	go func() {
		defer wg.Done()
		ownedLicenses, ownedErr = chainClient.GetOwnedLicenses(context.Background(), ownerAddr.String())
	}()
	go func() {
		defer wg.Done()
		delegatedLicenses, delegatedErr = chainClient.GetDelegatedLicenses(context.Background(), ownerAddr.String())
	}()
	wg.Wait()

	// Process results
	if ownedErr != nil {
		log.Printf("Warning: Failed to query owned licenses: %v", ownedErr)
	} else if len(ownedLicenses) > 0 {
		log.Printf("Auto-detected %d owned license(s)", len(ownedLicenses))
		licenses = append(licenses, ownedLicenses...)
	}

	if delegatedErr != nil {
		log.Printf("Warning: Failed to query delegated licenses: %v", delegatedErr)
	} else if len(delegatedLicenses) > 0 {
		log.Printf("Auto-detected %d delegated license(s)", len(delegatedLicenses))
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
	}

	// Optional sanity: compare configured owner VRF key with chain state when present
	if keyInfo, err := chainClient.GetOwnerKeyInfo(context.Background(), ownerAddr.String()); err == nil && keyInfo != nil && len(keyInfo.VrfPubkey) == 32 {
		if !bytes.Equal(keyInfo.VrfPubkey, vrfPubKey) {
			log.Printf("Warning: local VRF pubkey differs from on-chain owner key (nonce %d)", keyInfo.Nonce)
		}
	}

	return m, nil
}

// loadVRFKeyFromEnv loads VRF key from MINER_VRF_KEY environment variable
// Note: Uses os.Getenv directly instead of config.Get() to support auto mode
// where the VRF key is set via os.Setenv after config.Load() has already run.
func loadVRFKeyFromEnv() ([]byte, []byte, error) {
	// Read directly from env to support auto mode (key set after config loaded)
	hexKey := os.Getenv(config.EnvVRFKey)
	if hexKey == "" {
		// Fallback to config in case someone sets it there
		hexKey = config.Get().VRFKey
	}
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

	// Expose operator address as info gauge for label joins in Prometheus/Grafana.
	metricOperatorInfo.WithLabelValues(m.ownerAddr.String()).Set(1)

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

// processEpoch processes a new epoch for all licenses
func (m *MinerManager) processEpoch(ctx context.Context, epochInfo *minertypes.QueryEpochResponse) {
	// Re-check freshness before spending CPU so a lagging RPC event cannot keep
	// the miner working on an epoch the chain has already passed.
	currentEpoch, err := m.chainClient.GetCurrentEpoch(ctx)
	if err != nil {
		log.Printf("Failed to query current epoch before processing epoch %d; processing queued epoch: %v", epochInfo.Epoch, err)
	} else if currentEpoch.Epoch > epochInfo.Epoch {
		log.Printf("Epoch advanced from %d to %d before processing; jumping to current epoch", epochInfo.Epoch, currentEpoch.Epoch)
		epochInfo = currentEpoch
	}

	if m.stats.StartEpoch == 0 {
		m.stats.StartEpoch = epochInfo.Epoch
		atomic.StoreUint64(&m.stats.LastProcessedEpoch, 0)
		log.Printf("Mining session started at epoch %d", epochInfo.Epoch)
	}

	// Query eligible licenses (delegated-licenses returns all minable licenses for this operator)
	eligibleLicenses, err := m.chainClient.GetDelegatedLicenses(ctx, m.ownerAddr.String())
	if err != nil {
		log.Printf("Failed to query eligible licenses: %v", err)
		return
	}

	if len(eligibleLicenses) == 0 {
		log.Printf("No eligible licenses for epoch %d", epochInfo.Epoch)
		return
	}

	// Log epoch processing
	log.Printf("Processing epoch %d with %d eligible licenses (seed: %x)",
		epochInfo.Epoch, len(eligibleLicenses), epochInfo.Seed[:8])

	batchSize := config.Get().BatchSize

	// Process licenses in parallel and submit batches as soon as they're ready
	resultsChan := make(chan *minertypes.WorkSubmission, len(eligibleLicenses))
	var processWg sync.WaitGroup

	// Start workers to process licenses
	for _, licenseID := range eligibleLicenses {
		processWg.Add(1)
		go func(lid uint64) {
			defer processWg.Done()
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
		processWg.Wait()
		close(resultsChan)
	}()

	// Collect results and submit batches immediately when batch is full
	// Note: submissions are sequential to avoid nonce conflicts
	var currentBatch []minertypes.WorkSubmission
	batchNum := 0
	totalWins := 0
	epochExpired := false

	for result := range resultsChan {
		currentBatch = append(currentBatch, *result)
		totalWins++

		// When batch is full, submit immediately
		if len(currentBatch) >= batchSize {
			// Check if epoch is still valid before submitting
			if !epochExpired {
				currentEpoch, err := m.chainClient.GetCurrentEpoch(ctx)
				if err == nil && currentEpoch.Epoch != epochInfo.Epoch {
					log.Printf("⏰ Epoch %d expired (current: %d), skipping remaining batches", epochInfo.Epoch, currentEpoch.Epoch)
					epochExpired = true
				}
			}

			if epochExpired {
				currentBatch = currentBatch[:0] // Discard batch
				continue
			}

			batchNum++
			log.Printf("📦 Submitting batch %d (%d submissions)...", batchNum, len(currentBatch))
			m.submitBatchWork(ctx, currentBatch)
			currentBatch = currentBatch[:0] // Reset slice
		}
	}

	// Submit remaining results (if epoch not expired)
	if len(currentBatch) > 0 && !epochExpired {
		// Final check before last batch
		currentEpoch, err := m.chainClient.GetCurrentEpoch(ctx)
		if err == nil && currentEpoch.Epoch != epochInfo.Epoch {
			log.Printf("⏰ Epoch %d expired (current: %d), skipping final batch", epochInfo.Epoch, currentEpoch.Epoch)
		} else {
			batchNum++
			log.Printf("📦 Submitting final batch %d (%d submissions)...", batchNum, len(currentBatch))
			m.submitBatchWork(ctx, currentBatch)
		}
	}

	if totalWins > 0 {
		log.Printf("🎯 Total wins: %d licenses in %d batch(es)", totalWins, batchNum)
	} else {
		log.Printf("No wins this epoch")
	}

	// Update last processed epoch
	atomic.StoreUint64(&m.stats.LastProcessedEpoch, epochInfo.Epoch)
}

// processLicenseForBatch processes mining for a single license and returns result if won
func (m *MinerManager) processLicenseForBatch(ctx context.Context, licenseID uint64, epochInfo *minertypes.QueryEpochResponse) *minertypes.WorkSubmission {
	// Skip pre-check query to reduce latency - VRF computation is fast,
	// and chain will validate eligibility on submission anyway.
	// This avoids N gRPC queries per epoch (where N = number of licenses).

	// Get stats for this license (map is read-only after init, so safe)
	stats := m.stats.LicenseStats[licenseID]
	if stats != nil {
		atomic.AddUint64(&stats.VrfAttempts, 1)
		atomic.StoreUint64(&stats.CurrentEpoch, epochInfo.Epoch)
	}
	atomic.AddUint64(&m.stats.TotalAttempts, 1)
	metricVRFAttempts.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()

	// Build VRF input message
	vrfMsg := BuildVRFMessage(epochInfo.Seed, licenseID, m.ownerAddr)

	// Generate VRF proof
	vrfStart := time.Now()
	y, proof, err := GenerateVRFProof(m.vrfPrivKey, vrfMsg)
	if err != nil {
		log.Printf("License %d: VRF proof generation failed: %v", licenseID, err)
		log.Printf("💡 This may indicate a corrupted VRF key. Try regenerating with: ./aultmined keygen")
		metricVRFProofFailures.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()
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
	metricWins.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()
	log.Printf("🎯 License %d: VRF WIN! Starting PoW...", licenseID)

	// Solve proof-of-work with fixed difficulty (k=16 leading zero bits)
	kFixed := uint32(16)
	powStart := time.Now()
	nonce, err := SolvePoW(epochInfo.Seed, licenseID, y, kFixed)
	if err != nil {
		log.Printf("License %d: PoW failed after max attempts: %v", licenseID, err)
		log.Printf("💡 This is unusual - PoW should usually succeed. Check CPU availability")
		metricPoWFailures.WithLabelValues(strconv.FormatUint(licenseID, 10)).Inc()
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
	_, err := m.chainClient.BatchSubmitWork(ctx, workResults)
	if err != nil {
		log.Printf("❌ Failed to submit batch work: %v", err)
		var reason string
		if strings.Contains(err.Error(), "duplicate") {
			log.Printf("💡 Some licenses may have already submitted for this epoch")
			reason = "duplicate"
		} else if strings.Contains(err.Error(), "not eligible") {
			log.Printf("💡 Some licenses may be quarantined or not properly configured")
			reason = "not_eligible"
		} else if strings.Contains(err.Error(), "invalid proof") {
			log.Printf("💡 VRF key mismatch - ensure your key is registered on-chain")
			reason = "invalid_proof"
		} else if strings.Contains(err.Error(), "VRF verification failed") {
			log.Printf("🔍 Debug: VRF verification failed for some proofs")
			reason = "vrf_verification_failed"
		} else if strings.Contains(err.Error(), "key too young") || strings.Contains(err.Error(), "key registered at epoch") {
			log.Printf("⏱️  VRF key is too young - must wait a few epochs after registration before mining")
			reason = "key_too_young"
		} else if strings.Contains(err.Error(), "transaction not confirmed") {
			// Transaction was submitted but not confirmed - still count as submission
			for _, result := range workResults {
				if stats := m.stats.LicenseStats[result.LicenseId]; stats != nil {
					atomic.AddUint64(&stats.Submissions, 1)
				}
			}
			atomic.AddUint64(&m.stats.TotalSubmissions, uint64(len(workResults)))
			log.Printf("💡 Transaction submitted but confirmation timed out - check tx status manually")
			reason = "confirmation_timeout"
		} else {
			log.Printf("💡 Check chain connection and account balance for gas")
			reason = "send_failed"
		}
		// Aggregate by reason only; a per-license_id label would explode
		// cardinality on nodes with many delegated licenses.
		metricSubmitFailures.WithLabelValues(reason).Add(float64(len(workResults)))
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
	for _, r := range workResults {
		metricSubmissions.WithLabelValues(strconv.FormatUint(r.LicenseId, 10)).Inc()
	}
}
