package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// GetCurrentEpoch queries the current epoch from chain
func (c *ChainClient) GetCurrentEpoch(ctx context.Context) (*minertypes.QueryEpochResponse, error) {
	if len(c.grpcEndpoints) > 1 {
		return c.getHighestCurrentEpoch(ctx)
	}

	var lastErr error
	for range len(c.grpcEndpoints) {
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

func (c *ChainClient) getHighestCurrentEpoch(ctx context.Context) (*minertypes.QueryEpochResponse, error) {
	c.endpointMu.RLock()
	endpoints := append([]grpcEndpointConfig(nil), c.grpcEndpoints...)
	primaryQueryClient := c.queryClient
	c.endpointMu.RUnlock()

	type epochResult struct {
		resp *minertypes.QueryEpochResponse
		err  error
	}

	results := make(chan epochResult, len(endpoints))
	var wg sync.WaitGroup

	for i, endpoint := range endpoints {
		wg.Add(1)
		go func(i int, endpoint grpcEndpointConfig) {
			defer wg.Done()

			queryClient := primaryQueryClient
			if i > 0 {
				var err error
				queryClient, err = c.getEpochQueryClient(endpoint)
				if err != nil {
					results <- epochResult{err: err}
					return
				}
			}

			resp, err := queryEpochWithRetries(ctx, queryClient)
			results <- epochResult{resp: resp, err: err}
		}(i, endpoint)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var highest *minertypes.QueryEpochResponse
	var queryErrs []error

	for result := range results {
		if result.err != nil {
			queryErrs = append(queryErrs, result.err)
			continue
		}
		if highest == nil || result.resp.Epoch > highest.Epoch {
			highest = result.resp
		}
	}

	if highest != nil {
		return highest, nil
	}
	if err := errors.Join(queryErrs...); err != nil {
		return nil, fmt.Errorf("failed to query epoch: %w", err)
	}
	return nil, fmt.Errorf("failed to query epoch")
}

func (c *ChainClient) getEpochQueryClient(endpoint grpcEndpointConfig) (minertypes.QueryClient, error) {
	c.epochMu.Lock()
	defer c.epochMu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("chain client is closed")
	}

	if queryClient := c.epochQueryClients[endpoint]; queryClient != nil {
		return queryClient, nil
	}

	conn, err := newGRPCConn(endpoint, c.protoCodec)
	if err != nil {
		return nil, err
	}

	queryClient := minertypes.NewQueryClient(conn)
	c.epochQueryConns[endpoint] = conn
	c.epochQueryClients[endpoint] = queryClient
	return queryClient, nil
}

func queryEpochWithRetries(ctx context.Context, queryClient minertypes.QueryClient) (*minertypes.QueryEpochResponse, error) {
	var lastErr error
	for range rpcMaxRetries {
		resp, err := queryClient.Epoch(ctx, &minertypes.QueryEpochRequest{})
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, fmt.Errorf("failed to query epoch: %w", ctx.Err())
		}
		time.Sleep(rpcRetryDelay)
	}
	return nil, lastErr
}

// GetLicenseMinerInfo queries mining info for a license
func (c *ChainClient) GetLicenseMinerInfo(ctx context.Context, licenseID uint64) (*minertypes.QueryLicenseMinerInfoResponse, error) {
	var lastErr error
	for range len(c.grpcEndpoints) {
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
	for range len(c.grpcEndpoints) {
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
	for range len(c.grpcEndpoints) {
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

// GetOwnedLicenses queries all license IDs owned by an address (parallel with concurrency limit)
func (c *ChainClient) GetOwnedLicenses(ctx context.Context, ownerAddr string) ([]uint64, error) {
	// First get the balance to know how many licenses to query
	balanceResp, err := c.licenseClient.BalanceOf(ctx, &licensetypes.QueryBalanceRequest{Owner: ownerAddr})
	if err != nil {
		return nil, fmt.Errorf("failed to query license balance: %w", err)
	}

	if balanceResp.Balance == 0 {
		return []uint64{}, nil
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
	licenses := make([]uint64, 0, balanceResp.Balance)
	var failedIndices []uint64
	for r := range resultsChan {
		if r.err != nil {
			failedIndices = append(failedIndices, r.index)
			continue
		}
		licenses = append(licenses, r.id)
	}

	if len(failedIndices) > 0 {
		fmt.Printf("      ⚠️  Warning: Failed to query %d licenses at indices: %v\n", len(failedIndices), failedIndices)
	}

	return licenses, nil
}

// GetDelegatedLicenses queries all license IDs delegated to an operator
func (c *ChainClient) GetDelegatedLicenses(ctx context.Context, operatorAddr string) ([]uint64, error) {
	var allLicenses []uint64
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
			return nil, fmt.Errorf("failed to query delegated licenses: %w", err)
		}

		allLicenses = append(allLicenses, resp.LicenseIds...)

		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		nextKey = resp.Pagination.NextKey
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
