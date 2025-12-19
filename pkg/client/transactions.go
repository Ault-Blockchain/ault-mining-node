package client

import (
	"context"
	"fmt"
	"log"
	"time"

	tmhash "github.com/cometbft/cometbft/crypto/tmhash"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"

	"github.com/Ault-Blockchain/ault/x/miner/keeper"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

const (
	txMaxRetries    = 3
	txRetryDelay    = 500 * time.Millisecond
	txGasAdjustment = 1.2
	txTimeout       = 30 * time.Second
)

// BatchSubmitWork creates and broadcasts a MsgBatchSubmitWork transaction
func (c *ChainClient) BatchSubmitWork(ctx context.Context, workResults []minertypes.WorkSubmission) (string, error) {
	if len(workResults) == 0 {
		return "", fmt.Errorf("no work results to submit")
	}

	fromAddr, err := c.GetOwnerAddress()
	if err != nil {
		return "", err
	}

	submissions := workResults

	msg := &minertypes.MsgBatchSubmitWork{
		Submissions: submissions,
		Submitter:   fromAddr.String(),
	}

	// Calculate gas limit for batch transaction
	// Free gas limit is 200,000 per submission (FreeMiningMaxGasLimit)
	// Stay within free gas limit: gasLimit <= 200000 * submissionCount
	gasLimit := uint64(len(submissions) * 200000)
	if gasLimit > 5000000 {
		gasLimit = 5000000
	}

	txHash, err := c.broadcastTransaction(ctx, fromAddr, msg, gasLimit)
	if err != nil {
		return "", err
	}
	fmt.Printf("Batch work submitted successfully! Tx: %s\n", txHash)
	return txHash, nil
}

// SetOwnerVRFKey sets the VRF key for the owner
func (c *ChainClient) SetOwnerVRFKey(ctx context.Context, vrfPubkey []byte, nonce uint64, autoPoP bool) error {
	fromAddr, err := c.GetOwnerAddress()
	if err != nil {
		return err
	}

	var pop []byte
	if autoPoP {
		// Query the current epoch from the chain
		epochRes, err := c.queryClient.Epoch(ctx, &minertypes.QueryEpochRequest{})
		if err != nil {
			return fmt.Errorf("failed to query current epoch: %w", err)
		}
		currentEpoch := epochRes.Epoch

		// Build PoP message
		msgBytes := keeper.BuildOwnerKeyPoPWithChainID(c.chainID, fromAddr, vrfPubkey, currentEpoch, nonce)

		// Sign with operator private key
		sig, err := c.privKey.Sign(msgBytes)
		if err != nil {
			return fmt.Errorf("failed to sign PoP: %w", err)
		}
		if len(sig) != 65 {
			return fmt.Errorf("unexpected signature length: got %d bytes, expected 65", len(sig))
		}
		// Normalize recovery id
		v := sig[64]
		if v >= 27 && v <= 30 {
			sig[64] = v - 27
		} else if v >= 35 {
			sig[64] = (v - 35) % 2
		} else if v > 3 {
			return fmt.Errorf("invalid recovery ID %d in signature", v)
		}
		pop = sig
	} else {
		pop = make([]byte, 0)
	}

	msg := &minertypes.MsgSetOwnerVrfKey{
		VrfPubkey:       vrfPubkey,
		PossessionProof: pop,
		Nonce:           nonce,
		Owner:           fromAddr.String(),
	}

	txHash, err := c.broadcastTransaction(ctx, fromAddr, msg, 200000)
	if err != nil {
		return err
	}
	fmt.Printf("Owner VRF key set! Tx: %s\n", txHash)
	return c.waitForVRFKeyRegistration(ctx, fromAddr.String(), vrfPubkey)
}

// waitForVRFKeyRegistration polls the chain to verify VRF key registration
func (c *ChainClient) waitForVRFKeyRegistration(ctx context.Context, ownerAddr string, expectedPubkey []byte) error {
	const (
		pollInterval = 3 * time.Second
		maxAttempts  = 10
	)

	for i := 0; i < maxAttempts; i++ {
		keyInfo, err := c.GetOwnerKeyInfo(ctx, ownerAddr)
		if err == nil && keyInfo != nil && len(keyInfo.VrfPubkey) > 0 {
			if len(expectedPubkey) == len(keyInfo.VrfPubkey) {
				match := true
				for j := range expectedPubkey {
					if expectedPubkey[j] != keyInfo.VrfPubkey[j] {
						match = false
						break
					}
				}
				if match {
					return nil
				}
			}
		}

		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
			}
		}
	}
	return fmt.Errorf("VRF key registration not confirmed after %d seconds", maxAttempts*int(pollInterval.Seconds()))
}

// buildSignAndBroadcast builds, signs and broadcasts a tx with one msg
func (c *ChainClient) buildSignAndBroadcast(ctx context.Context, fromAddr sdk.AccAddress, msg sdk.Msg, gasLimit uint64) (string, error) {
	// Serialize signing/broadcasting to avoid concurrent sequence races
	c.mu.Lock()
	defer c.mu.Unlock()

	// Ensure local account number/sequence cache is initialized
	if err := c.refreshAccountSequence(ctx, fromAddr, false); err != nil {
		return "", err
	}

	// Retry loop with sequence refresh/backoff on mismatch
	var (
		txHash  string
		lastErr error
	)

	for attempt := 0; attempt <= txMaxRetries; attempt++ {
		// Check free gas eligibility FIRST with original gas limit
		// This must be done before gas adjustment to stay within FreeMiningMaxGasLimit
		var estGas uint64
		var fees sdk.Coins
		var floor sdkmath.LegacyDec
		var simGas uint64
		useFreeGas := c.isFreeGasEligible(ctx, gasLimit)

		if useFreeGas {
			// Use original gas limit to stay within free gas limit (no 1.2x adjustment)
			estGas = gasLimit
			simGas = gasLimit
			fees = sdk.NewCoins()
		} else {
			// Simulate gas and apply adjustment for paid transactions
			var simErr error
			simGas, simErr = c.simulateGas(ctx, fromAddr, msg)
			if simErr != nil {
				simGas = gasLimit
			}
			estGas = uint64(float64(simGas)*txGasAdjustment + 0.9999)
			if estGas < gasLimit {
				estGas = gasLimit
			}
			var feeErr error
			fees, floor, feeErr = c.computeFees(estGas)
			if feeErr != nil {
				return "", feeErr
			}
		}

		// Build tx fresh each attempt
		builder := c.txConfig.NewTxBuilder()
		if err := builder.SetMsgs(msg); err != nil {
			return "", fmt.Errorf("failed to set msg: %w", err)
		}
		builder.SetGasLimit(estGas)
		builder.SetFeeAmount(fees)

		// Sign using private key
		extBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
		if !ok {
			return "", fmt.Errorf("tx builder does not support extensions")
		}
		if err := c.signTx(extBuilder, c.nextSeq); err != nil {
			return "", fmt.Errorf("failed to sign tx: %w", err)
		}

		// Encode and broadcast with timeout
		txBytes, err := c.txConfig.TxEncoder()(builder.GetTx())
		if err != nil {
			return "", fmt.Errorf("failed to encode tx: %w", err)
		}

		// Precompute tx hash for idempotency checks on network errors
		preHash := fmt.Sprintf("%X", tmhash.Sum(txBytes))
		mode := txtypes.BroadcastMode_BROADCAST_MODE_SYNC

		// Concise logging of gas/fees/mode/seq
		if attempt == 0 {
			if useFreeGas {
				log.Printf("tx gas: used=%d fee=FREE mode=%s seq=%d", estGas, mode.String(), c.nextSeq)
			} else if !floor.IsZero() {
				log.Printf("tx gas: sim=%d adj=%.2f used=%d fee=%s floor=%s mode=%s seq=%d", simGas, txGasAdjustment, estGas, fees.String(), floor.String(), mode.String(), c.nextSeq)
			} else {
				log.Printf("tx gas: sim=%d adj=%.2f used=%d fee=%s mode=%s seq=%d", simGas, txGasAdjustment, estGas, fees.String(), mode.String(), c.nextSeq)
			}
		} else {
			log.Printf("retry #%d: gas used=%d fee=%s mode=%s seq=%d", attempt, estGas, fees.String(), mode.String(), c.nextSeq)
		}
		bctx, cancel := context.WithTimeout(ctx, txTimeout)
		defer cancel()
		resp, err := c.txClient.BroadcastTx(bctx, &txtypes.BroadcastTxRequest{TxBytes: txBytes, Mode: mode})
		if err != nil {
			lastErr = fmt.Errorf("broadcast error: %w", err)
			if ok, derr := c.detectDelivered(bctx, preHash); derr == nil && ok {
				return preHash, nil
			}
		} else if resp.TxResponse != nil {
			if resp.TxResponse.Code == 0 {
				c.nextSeq++
				return resp.TxResponse.TxHash, nil
			}

			raw := resp.TxResponse.RawLog
			if expected, got, ok := parseSequenceMismatch(raw); ok {
				c.nextSeq = expected
				lastErr = fmt.Errorf("sequence mismatch (expected %d, got %d)", expected, got)
			} else if isOutOfGas(raw) {
				gasLimit = uint64(float64(estGas)*1.3 + 0.9999)
				log.Printf("out of gas reported; increasing gas to %d and retrying", gasLimit)
				lastErr = fmt.Errorf("out of gas, retrying with higher limit")
			} else if isDuplicateTx(raw) {
				txHash = resp.TxResponse.TxHash
				if txHash == "" {
					txHash = preHash
				}
				if werr := c.waitForTxConfirmation(ctx, txHash); werr == nil {
					c.nextSeq++
					return txHash, nil
				}
				lastErr = fmt.Errorf("duplicate tx reported but not confirmed yet")
			} else {
				lastErr = fmt.Errorf("tx failed code=%d: %s", resp.TxResponse.Code, raw)
			}
		} else {
			lastErr = fmt.Errorf("empty tx response")
		}

		// Decide whether to retry
		if attempt < txMaxRetries {
			_ = c.refreshAccountSequence(ctx, fromAddr, true)
			time.Sleep(txRetryDelay)
			continue
		}
		break
	}

	if lastErr != nil {
		return "", lastErr
	}
	return txHash, fmt.Errorf("broadcast failed without explicit error")
}

// broadcastTransaction broadcasts a transaction using operator funds
// Note: Free gas is automatically granted by the chain for eligible miner operations
func (c *ChainClient) broadcastTransaction(ctx context.Context, fromAddr sdk.AccAddress, msg sdk.Msg, gasLimit uint64) (string, error) {
	return c.buildSignAndBroadcast(ctx, fromAddr, msg, gasLimit)
}
