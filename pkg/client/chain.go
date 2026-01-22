// Package client provides blockchain client functionality for the miner.
// It handles chain interactions including queries, transactions, and account management.
package client

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	cmthttp "github.com/cometbft/cometbft/rpc/client/http"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	cryptocodec "github.com/cosmos/evm/crypto/codec"
	"github.com/cosmos/evm/crypto/ethsecp256k1"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	appcfg "github.com/Ault-Blockchain/ault/app/config"
	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

// NewChainClient creates a new chain client with connection management.
// It reads the operator key from the MINER_OPERATOR_KEY environment variable.
func NewChainClient() (*ChainClient, error) {
	cfg := config.Get()
	if cfg.OperatorKey == "" {
		return nil, fmt.Errorf("MINER_OPERATOR_KEY environment variable is required")
	}
	return NewChainClientWithKey(cfg.OperatorKey)
}

// NewChainClientWithKey creates a chain client using an explicit operator key (for auto mode)
func NewChainClientWithKey(operatorKeyHex string) (*ChainClient, error) {
	cfg := config.Get()
	grpcEndpoint, useTLS, serverName, err := normalizeGRPCEndpoint(cfg.GRPCEndpoint)
	if err != nil {
		return nil, err
	}
	rpcEndpoint := cfg.RPCEndpoint

	if operatorKeyHex == "" {
		return nil, fmt.Errorf("operator key is required")
	}

	privKeyBytes, err := hex.DecodeString(operatorKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid operator key hex: %w", err)
	}

	privKey := &ethsecp256k1.PrivKey{Key: privKeyBytes}
	pubKey := privKey.PubKey()
	address := sdk.AccAddress(pubKey.Address())

	log.Printf("Operator address: %s", address.String())

	// Gas prices
	gasPrices := "10000000" + appcfg.AttoDenom

	// Create interface registry and codec for protobuf handling
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	minertypes.RegisterInterfaces(interfaceRegistry)
	licensetypes.RegisterInterfaces(interfaceRegistry)
	authtypes.RegisterInterfaces(interfaceRegistry)
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	protoCodec := codec.NewProtoCodec(interfaceRegistry)

	// Tx config for building/signing transactions
	txCfg := authtx.NewTxConfig(protoCodec, authtx.DefaultSignModes)

	// Connect with retry and keep-alive options
	var transportCreds credentials.TransportCredentials
	if useTLS {
		transportCreds = credentials.NewTLS(&tls.Config{ServerName: serverName})
	} else {
		transportCreds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(protoCodec.GRPCCodec()),
			grpc.MaxCallRecvMsgSize(10*1024*1024),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  1.0 * time.Second,
				Multiplier: 1.5,
				Jitter:     0.2,
				MaxDelay:   10 * time.Second,
			},
			MinConnectTimeout: 5 * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC: %w", err)
	}

	client := &ChainClient{
		grpcConn:          conn,
		queryClient:       minertypes.NewQueryClient(conn),
		licenseClient:     licensetypes.NewQueryClient(conn),
		authClient:        authtypes.NewQueryClient(conn),
		txClient:          txtypes.NewServiceClient(conn),
		feemarketClient:   feemarkettypes.NewQueryClient(conn),
		grpcEndpoint:      grpcEndpoint,
		rpcEndpoint:       rpcEndpoint,
		chainID:           cfg.ChainID,
		gasPrices:         gasPrices,
		privKey:           privKey,
		address:           address,
		interfaceRegistry: interfaceRegistry,
		protoCodec:        protoCodec,
		txConfig:          txCfg,
	}

	// Discover chain ID from node status
	if nid, err := client.discoverChainID(); err == nil && nid != "" {
		log.Printf("Discovered chain ID: %s", nid)
		client.chainID = nid
	}

	return client, nil
}

// GetOwnerAddress returns the operator's address
func (c *ChainClient) GetOwnerAddress() (sdk.AccAddress, error) {
	return c.address, nil
}

// Close closes the gRPC connection
func (c *ChainClient) Close() {
	if c.grpcConn != nil {
		c.grpcConn.Close()
	}
}

// GetChainID returns the chain ID
func (c *ChainClient) GetChainID() string {
	return c.chainID
}

// setSignature sets a signature on the tx builder using the private key
func (c *ChainClient) setSignature(builder authtx.ExtensionOptionsTxBuilder, sequence uint64) error {
	sig := signingtypes.SignatureV2{
		PubKey:   c.privKey.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: sequence,
	}
	return builder.SetSignatures(sig)
}

// signTx signs the transaction using the private key
func (c *ChainClient) signTx(builder authtx.ExtensionOptionsTxBuilder, sequence uint64) error {
	pubKey := c.privKey.PubKey()

	// Set empty signature first to get the sign bytes
	sig := signingtypes.SignatureV2{
		PubKey: pubKey,
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signingtypes.SignMode_SIGN_MODE_DIRECT,
			Signature: nil,
		},
		Sequence: sequence,
	}
	if err := builder.SetSignatures(sig); err != nil {
		return fmt.Errorf("failed to set empty signature: %w", err)
	}

	// Get sign bytes
	signerData := authsigning.SignerData{
		ChainID:       c.chainID,
		AccountNumber: c.accNum,
		Sequence:      sequence,
		PubKey:        pubKey,
		Address:       c.address.String(),
	}
	log.Printf("Signing with chainID=%s accNum=%d seq=%d", c.chainID, c.accNum, sequence)

	signBytes, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		c.txConfig.SignModeHandler(),
		signingtypes.SignMode_SIGN_MODE_DIRECT,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return fmt.Errorf("failed to get sign bytes: %w", err)
	}

	// Sign with private key
	signature, err := c.privKey.Sign(signBytes)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	// Set the actual signature
	sig = signingtypes.SignatureV2{
		PubKey: pubKey,
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signingtypes.SignMode_SIGN_MODE_DIRECT,
			Signature: signature,
		},
		Sequence: sequence,
	}
	return builder.SetSignatures(sig)
}

// waitForTxConfirmation waits for a transaction to be included in a block
func (c *ChainClient) waitForTxConfirmation(ctx context.Context, txHash string) error {
	const (
		pollInterval = 3 * time.Second
		maxAttempts  = 10 // 30s / 3s = 10
	)

	for i := 0; i < maxAttempts; i++ {
		resp, err := c.txClient.GetTx(ctx, &txtypes.GetTxRequest{Hash: txHash})
		if err == nil && resp != nil && resp.TxResponse != nil {
			if resp.TxResponse.Code == 0 {
				return nil
			}
			return fmt.Errorf("transaction failed with code %d: %s", resp.TxResponse.Code, resp.TxResponse.RawLog)
		}

		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
			}
		}
	}
	return fmt.Errorf("transaction not confirmed after %d seconds", maxAttempts*int(pollInterval.Seconds()))
}

// discoverChainID queries the node for the chain ID
func (c *ChainClient) discoverChainID() (string, error) {
	cli, err := cmthttp.New(c.rpcEndpoint, "/websocket")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := cli.Status(ctx)
	if err != nil {
		return "", err
	}
	return status.NodeInfo.Network, nil
}

// refreshAccountSequence refreshes account number and sequence from chain
func (c *ChainClient) refreshAccountSequence(ctx context.Context, addr sdk.AccAddress, force bool) error {
	if !force && c.seqInit && time.Since(c.lastSeqSync) < 400*time.Millisecond {
		return nil
	}

	accRes, err := c.authClient.Account(ctx, &authtypes.QueryAccountRequest{Address: addr.String()})
	if err != nil {
		return fmt.Errorf("failed to query account: %w", err)
	}
	var accI sdk.AccountI
	if err := c.interfaceRegistry.UnpackAny(accRes.Account, &accI); err != nil {
		return fmt.Errorf("failed to unpack account: %w", err)
	}
	onChainAccNum := accI.GetAccountNumber()
	onChainSeq := accI.GetSequence()
	log.Printf("Account query: addr=%s accNum=%d seq=%d", addr.String(), onChainAccNum, onChainSeq)

	if !c.seqInit || force {
		c.accNum = onChainAccNum
		c.nextSeq = onChainSeq
		c.seqInit = true
	} else {
		c.accNum = onChainAccNum
		if c.nextSeq < onChainSeq {
			c.nextSeq = onChainSeq
		}
	}
	c.lastSeqSync = time.Now()
	return nil
}

// computeFees computes transaction fees based on gas and gas prices
func (c *ChainClient) computeFees(gas uint64) (sdk.Coins, sdkmath.LegacyDec, error) {
	decs, err := sdk.ParseDecCoins(c.gasPrices)
	if err != nil {
		return nil, sdkmath.LegacyDec{}, fmt.Errorf("invalid gas prices: %w", err)
	}

	// Dynamic fee floor via feemarket params
	const minGasPriceMargin = 1.1
	var floor sdkmath.LegacyDec
	if resp, err := c.feemarketClient.Params(context.Background(), &feemarkettypes.QueryParamsRequest{}); err == nil && resp != nil {
		floor = resp.Params.MinGasPrice
		if !floor.IsZero() {
			mStr := fmt.Sprintf("%.6f", minGasPriceMargin)
			if m, err := sdkmath.LegacyNewDecFromStr(mStr); err == nil {
				floor = floor.Mul(m)
			}
		}
	}

	// Enforce floor if present
	if floor.IsPositive() {
		for i := range decs {
			if decs[i].Amount.LT(floor) {
				decs[i].Amount = floor
			}
		}
	}

	fees := sdk.NewCoins()
	g := sdkmath.NewIntFromUint64(gas)
	for _, dc := range decs {
		amt := dc.Amount.MulInt(g).Ceil().TruncateInt()
		if !amt.IsZero() {
			fees = fees.Add(sdk.NewCoin(dc.Denom, amt))
		}
	}
	if fees.IsZero() && decs.Len() > 0 {
		fees = fees.Add(sdk.NewCoin(decs[0].Denom, sdkmath.NewInt(1)))
	}
	return fees, floor, nil
}

// isFreeGasEligible checks if the current epoch is eligible for free gas
// gasLimit is total gas, submissionCount is number of submissions in batch
func (c *ChainClient) isFreeGasEligible(ctx context.Context, gasLimit uint64, submissionCount int) bool {
	params, err := c.GetParams(ctx)
	if err != nil {
		return false
	}

	epochResp, err := c.GetCurrentEpoch(ctx)
	if err != nil {
		return false
	}

	// Check if current epoch is before free mining cutoff
	if epochResp.Epoch >= params.FreeMiningUntilEpoch {
		return false
	}

	// Check if gas limit is within free gas limit (FreeMiningMaxGasLimit is per submission)
	maxAllowedGas := params.FreeMiningMaxGasLimit * uint64(submissionCount)
	if submissionCount == 0 {
		maxAllowedGas = params.FreeMiningMaxGasLimit
	}
	if gasLimit > maxAllowedGas {
		return false
	}

	return true
}

// simulateGas estimates gas via Service.Simulate
func (c *ChainClient) simulateGas(ctx context.Context, fromAddr sdk.AccAddress, msg sdk.Msg) (uint64, error) {
	if err := c.refreshAccountSequence(ctx, fromAddr, false); err != nil {
		return 0, err
	}

	builder := c.txConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		return 0, fmt.Errorf("failed to set msg: %w", err)
	}
	builder.SetFeeAmount(sdk.NewCoins())
	builder.SetGasLimit(0)

	// Set signature with pubkey for simulation
	extBuilder, ok := builder.(authtx.ExtensionOptionsTxBuilder)
	if !ok {
		return 0, fmt.Errorf("tx builder does not support extensions")
	}
	if err := c.setSignature(extBuilder, c.nextSeq); err != nil {
		return 0, err
	}

	txBytes, err := c.txConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		return 0, fmt.Errorf("failed to encode tx for simulate: %w", err)
	}

	sctx, cancel := context.WithTimeout(ctx, txTimeout)
	defer cancel()
	sim, err := c.txClient.Simulate(sctx, &txtypes.SimulateRequest{TxBytes: txBytes})
	if err != nil {
		return 0, err
	}
	if sim.GetGasInfo() == nil {
		return 0, fmt.Errorf("simulate returned no gas info")
	}
	used := sim.GasInfo.GasUsed
	if used == 0 {
		used = 200000
	}
	return used, nil
}

// detectDelivered checks whether a tx with the given hash is already indexed
func (c *ChainClient) detectDelivered(ctx context.Context, txHash string) (bool, error) {
	if txHash == "" {
		return false, nil
	}
	resp, err := c.txClient.GetTx(ctx, &txtypes.GetTxRequest{Hash: txHash})
	if err != nil {
		return false, nil
	}
	if resp != nil && resp.TxResponse != nil {
		return resp.TxResponse.Code == 0, nil
	}
	return false, nil
}

var (
	seqMismatchRe1 = regexp.MustCompile(`account sequence mismatch, expected (\d+), got (\d+)`)
	seqMismatchRe2 = regexp.MustCompile(`wrong sequence.*expected (\d+), got (\d+)`)
	seqMismatchRe3 = regexp.MustCompile(`incorrect account sequence.*expected (\d+), got (\d+)`)
)

func parseSequenceMismatch(raw string) (expected, got uint64, ok bool) {
	if m := seqMismatchRe1.FindStringSubmatch(raw); len(m) == 3 {
		exp, _ := strconv.ParseUint(m[1], 10, 64)
		g, _ := strconv.ParseUint(m[2], 10, 64)
		return exp, g, true
	}
	if m := seqMismatchRe2.FindStringSubmatch(strings.ToLower(raw)); len(m) == 3 {
		exp, _ := strconv.ParseUint(m[1], 10, 64)
		g, _ := strconv.ParseUint(m[2], 10, 64)
		return exp, g, true
	}
	if m := seqMismatchRe3.FindStringSubmatch(strings.ToLower(raw)); len(m) == 3 {
		exp, _ := strconv.ParseUint(m[1], 10, 64)
		g, _ := strconv.ParseUint(m[2], 10, 64)
		return exp, g, true
	}
	return 0, 0, false
}

func isDuplicateTx(raw string) bool {
	s := strings.ToLower(raw)
	return strings.Contains(s, "already in cache") || strings.Contains(s, "already exists") || strings.Contains(s, "duplicate")
}

func isOutOfGas(raw string) bool {
	s := strings.ToLower(raw)
	return strings.Contains(s, "out of gas") || strings.Contains(s, "insufficient gas")
}

// normalizeGRPCEndpoint parses CHAIN_GRPC and returns host:port plus TLS hints.
func normalizeGRPCEndpoint(raw string) (endpoint string, useTLS bool, serverName string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, "", fmt.Errorf("gRPC endpoint is empty")
	}

	if strings.Contains(raw, "://") {
		return "", false, "", fmt.Errorf("invalid CHAIN_GRPC: use host[:port] without scheme (e.g. test-grpc.cloud.aultblockchain.xyz)")
	}

	endpoint = raw
	if !strings.Contains(endpoint, ":") {
		endpoint = endpoint + ":9090"
	}
	serverName = endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		serverName = h
	}
	return endpoint, false, serverName, nil
}
