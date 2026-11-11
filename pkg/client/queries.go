package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// GetCurrentEpoch queries the current epoch from chain
func (c *ChainClient) GetCurrentEpoch(ctx context.Context) (*minertypes.QueryEpochResponse, error) {
	var lastErr error
	for range c.grpcCandidateCount() {
		for range rpcMaxRetries {
			resp, err := c.queryClient.Epoch(ctx, &minertypes.QueryEpochRequest{})
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, fmt.Errorf("failed to query epoch: %w", err)
			}
			time.Sleep(rpcRetryDelay)
		}
		if !c.switchToNextGRPCEndpoint() {
			break
		}
	}
	return nil, fmt.Errorf("failed to query epoch: %w", lastErr)
}

// GetLicenseMinerInfo queries mining info for a license
func (c *ChainClient) GetLicenseMinerInfo(ctx context.Context, licenseID uint64) (*minertypes.QueryLicenseMinerInfoResponse, error) {
	var lastErr error
	for range c.grpcCandidateCount() {
		for range rpcMaxRetries {
			resp, err := c.queryClient.LicenseMinerInfo(ctx, &minertypes.QueryLicenseMinerInfoRequest{LicenseId: licenseID})
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, fmt.Errorf("failed to query license info: %w", err)
			}
			time.Sleep(rpcRetryDelay)
		}
		if !c.switchToNextGRPCEndpoint() {
			break
		}
	}
	return nil, fmt.Errorf("failed to query license info: %w", lastErr)
}

// GetParams queries the miner module parameters
func (c *ChainClient) GetParams(ctx context.Context) (*minertypes.Params, error) {
	var lastErr error
	for range c.grpcCandidateCount() {
		for range rpcMaxRetries {
			resp, err := c.queryClient.Params(ctx, &minertypes.QueryParamsRequest{})
			if err == nil {
				return &resp.Params, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, fmt.Errorf("failed to query params: %w", err)
			}
			time.Sleep(rpcRetryDelay)
		}
		if !c.switchToNextGRPCEndpoint() {
			break
		}
	}
	return nil, fmt.Errorf("failed to query params: %w", lastErr)
}

// GetOwnerKeyInfo queries the VRF key info for an owner
func (c *ChainClient) GetOwnerKeyInfo(ctx context.Context, ownerAddr string) (*minertypes.QueryOwnerKeyResponse, error) {
	var lastErr error
	for range c.grpcCandidateCount() {
		for range rpcMaxRetries {
			resp, err := c.queryClient.OwnerKey(ctx, &minertypes.QueryOwnerKeyRequest{Owner: ownerAddr})
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, fmt.Errorf("failed to query owner key info: %w", err)
			}
			time.Sleep(rpcRetryDelay)
		}
		if !c.switchToNextGRPCEndpoint() {
			break
		}
	}
	return nil, fmt.Errorf("failed to query owner key info: %w", lastErr)
}

func (c *ChainClient) withGRPCRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	candidateCount := c.grpcCandidateCount()
	if candidateCount == 0 {
		return fmt.Errorf("%s: no gRPC candidates available", operation)
	}

	for candidateAttempt := 0; candidateAttempt < candidateCount; candidateAttempt++ {
		for attempt := 0; attempt < rpcMaxRetries; attempt++ {
			err := fn()
			if err == nil {
				return nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return fmt.Errorf("%s: %w", operation, err)
			}
			if attempt < rpcMaxRetries-1 && rpcRetryDelay > 0 {
				select {
				case <-ctx.Done():
					return fmt.Errorf("%s: %w", operation, ctx.Err())
				case <-time.After(rpcRetryDelay):
				}
			}
		}
		if candidateAttempt == candidateCount-1 || !c.switchToNextGRPCEndpoint() {
			break
		}
	}

	if lastErr != nil {
		return fmt.Errorf("%s: %w", operation, lastErr)
	}
	return fmt.Errorf("%s: no gRPC attempts completed", operation)
}

// GetOwnedLicenses queries all license IDs owned by an address (parallel with concurrency limit)
func (c *ChainClient) GetOwnedLicenses(ctx context.Context, ownerAddr string) ([]uint64, error) {
	var licenses []uint64
	if err := c.withGRPCRetry(ctx, "failed to query license balance", func() error {
		// First get the balance to know how many licenses to query
		balanceResp, err := c.licenseClient.BalanceOf(ctx, &licensetypes.QueryBalanceRequest{Owner: ownerAddr})
		if err != nil {
			return err
		}

		if balanceResp.Balance == 0 {
			licenses = []uint64{}
			return nil
		}

		// Query license IDs in parallel (max 10 concurrent)
		type result struct {
			index uint64
			id    uint64
			err   error
		}

		const maxConcurrency = 10
		sem := make(chan struct{}, maxConcurrency)
		resultsChan := make(chan result, balanceResp.Balance)

		var wg sync.WaitGroup
		for i := uint64(0); i < balanceResp.Balance; i++ {
			wg.Add(1)
			go func(idx uint64) {
				defer wg.Done()
				sem <- struct{}{}        // Acquire
				defer func() { <-sem }() // Release

				tokenResp, err := c.licenseClient.TokenOfOwnerByIndex(ctx, &licensetypes.QueryTokenByOwnerIndexRequest{
					Owner: ownerAddr,
					Index: idx,
				})
				if err != nil {
					resultsChan <- result{index: idx, err: err}
					return
				}
				resultsChan <- result{index: idx, id: tokenResp.Id}
			}(i)
		}

		// Close results channel when all goroutines complete
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Collect results
		nextLicenses := make([]uint64, 0, balanceResp.Balance)
		var failedIndices []uint64
		for r := range resultsChan {
			if r.err != nil {
				failedIndices = append(failedIndices, r.index)
				continue
			}
			nextLicenses = append(nextLicenses, r.id)
		}

		if len(failedIndices) > 0 {
			fmt.Printf("      ⚠️  Warning: Failed to query %d licenses at indices: %v\n", len(failedIndices), failedIndices)
		}

		licenses = nextLicenses
		return nil
	}); err != nil {
		return nil, err
	}

	return licenses, nil
}

// GetDelegatedLicenses queries all license IDs delegated to an operator
func (c *ChainClient) GetDelegatedLicenses(ctx context.Context, operatorAddr string) ([]uint64, error) {
	var allLicenses []uint64
	if err := c.withGRPCRetry(ctx, "failed to query delegated licenses", func() error {
		nextLicenses := []uint64(nil)
		var nextKey []byte

		for {
			resp, err := c.queryClient.DelegatedLicenses(ctx, &minertypes.QueryDelegatedLicensesRequest{
				Operator: operatorAddr,
				Pagination: &query.PageRequest{
					Key:   nextKey,
					Limit: 1000,
				},
			})
			if err != nil {
				return err
			}

			nextLicenses = append(nextLicenses, resp.LicenseIds...)

			if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
				break
			}
			nextKey = resp.Pagination.NextKey
		}

		allLicenses = nextLicenses
		return nil
	}); err != nil {
		return nil, err
	}

	return allLicenses, nil
}

// MonitorEpochs monitors for new epochs and sends them to a channel
func (c *ChainClient) MonitorEpochs(ctx context.Context, epochChan chan<- *minertypes.QueryEpochResponse) error {
	lastEpoch := uint64(0)
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			epoch, err := c.GetCurrentEpoch(ctx)
			if err != nil {
				fmt.Printf("Error querying epoch: %v\n", err)
				continue
			}

			if epoch.Epoch > lastEpoch {
				lastEpoch = epoch.Epoch
				select {
				case epochChan <- epoch:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}
