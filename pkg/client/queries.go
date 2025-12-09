package client

import (
	"context"
	"fmt"
	"time"

	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

// GetCurrentEpoch queries the current epoch from chain
func (c *ChainClient) GetCurrentEpoch(ctx context.Context) (*minertypes.QueryEpochResponse, error) {
	resp, err := c.queryClient.Epoch(ctx, &minertypes.QueryEpochRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to query epoch: %w", err)
	}
	// Note: The response already contains properly decoded values if using protobuf
	// If the values are still base64, they would need decoding here
	return resp, nil
}

// GetLicenseMinerInfo queries mining info for a license
func (c *ChainClient) GetLicenseMinerInfo(ctx context.Context, licenseID uint64) (*minertypes.QueryLicenseMinerInfoResponse, error) {
	resp, err := c.queryClient.LicenseMinerInfo(ctx, &minertypes.QueryLicenseMinerInfoRequest{
		LicenseId: licenseID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query license info: %w", err)
	}
	return resp, nil
}

// GetParams queries the miner module parameters
func (c *ChainClient) GetParams(ctx context.Context) (*minertypes.Params, error) {
	resp, err := c.queryClient.Params(ctx, &minertypes.QueryParamsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to query params: %w", err)
	}
	return &resp.Params, nil
}

// GetOwnerKeyInfo queries the VRF key info for an owner
func (c *ChainClient) GetOwnerKeyInfo(ctx context.Context, ownerAddr string) (*minertypes.QueryOwnerKeyResponse, error) {
	resp, err := c.queryClient.OwnerKey(ctx, &minertypes.QueryOwnerKeyRequest{
		Owner: ownerAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query owner key info: %w", err)
	}
	return resp, nil
}

// GetOwnedLicenses queries all license IDs owned by an address
func (c *ChainClient) GetOwnedLicenses(ctx context.Context, ownerAddr string) ([]uint64, error) {
	// First get the balance to know how many licenses to query
	balanceResp, err := c.licenseClient.BalanceOf(ctx, &licensetypes.QueryBalanceRequest{
		Owner: ownerAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query license balance: %w", err)
	}

	if balanceResp.Balance == 0 {
		return []uint64{}, nil
	}

	// Query each license ID by index
	licenses := make([]uint64, 0, balanceResp.Balance)
	var failedIndices []uint64
	for i := uint64(0); i < balanceResp.Balance; i++ {
		tokenResp, err := c.licenseClient.TokenOfOwnerByIndex(ctx, &licensetypes.QueryTokenByOwnerIndexRequest{
			Owner: ownerAddr,
			Index: i,
		})
		if err != nil {
			// Track failed queries but continue
			failedIndices = append(failedIndices, i)
			continue
		}
		licenses = append(licenses, tokenResp.Id)
	}

	// If we failed to query some licenses, include warning in error
	if len(failedIndices) > 0 {
		// Still return the licenses we could query, but with a warning
		fmt.Printf("      ⚠️  Warning: Failed to query %d licenses at indices: %v\n", len(failedIndices), failedIndices)
	}

	return licenses, nil
}

// GetDelegatedLicenses queries all license IDs delegated to an operator
func (c *ChainClient) GetDelegatedLicenses(ctx context.Context, operatorAddr string) ([]uint64, error) {
	resp, err := c.queryClient.DelegatedLicenses(ctx, &minertypes.QueryDelegatedLicensesRequest{
		Operator: operatorAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query delegated licenses: %w", err)
	}
	return resp.LicenseIds, nil
}

// GetLicensePayouts queries payouts for a license within epoch range
func (c *ChainClient) GetLicensePayouts(ctx context.Context, licenseID, fromEpoch, toEpoch uint64) (*minertypes.QueryLicensePayoutsResponse, error) {
	resp, err := c.queryClient.LicensePayouts(ctx, &minertypes.QueryLicensePayoutsRequest{
		LicenseId: licenseID,
		FromEpoch: fromEpoch,
		ToEpoch:   toEpoch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query license payouts: %w", err)
	}
	return resp, nil
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
