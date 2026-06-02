package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

type fakeMinerQueryClient struct {
	minertypes.QueryClient
	delegatedLicenses func(context.Context, *minertypes.QueryDelegatedLicensesRequest, ...grpc.CallOption) (*minertypes.QueryDelegatedLicensesResponse, error)
}

func (f fakeMinerQueryClient) DelegatedLicenses(ctx context.Context, in *minertypes.QueryDelegatedLicensesRequest, opts ...grpc.CallOption) (*minertypes.QueryDelegatedLicensesResponse, error) {
	return f.delegatedLicenses(ctx, in, opts...)
}

type fakeLicenseQueryClient struct {
	licensetypes.QueryClient
	balanceOf           func(context.Context, *licensetypes.QueryBalanceRequest, ...grpc.CallOption) (*licensetypes.QueryBalanceResponse, error)
	tokenOfOwnerByIndex func(context.Context, *licensetypes.QueryTokenByOwnerIndexRequest, ...grpc.CallOption) (*licensetypes.QueryTokenByOwnerIndexResponse, error)
}

func (f fakeLicenseQueryClient) BalanceOf(ctx context.Context, in *licensetypes.QueryBalanceRequest, opts ...grpc.CallOption) (*licensetypes.QueryBalanceResponse, error) {
	return f.balanceOf(ctx, in, opts...)
}

func (f fakeLicenseQueryClient) TokenOfOwnerByIndex(ctx context.Context, in *licensetypes.QueryTokenByOwnerIndexRequest, opts ...grpc.CallOption) (*licensetypes.QueryTokenByOwnerIndexResponse, error) {
	return f.tokenOfOwnerByIndex(ctx, in, opts...)
}

func withZeroGRPCRetryDelay(t *testing.T) {
	t.Helper()
	oldDelay := rpcRetryDelay
	rpcRetryDelay = 0
	t.Cleanup(func() {
		rpcRetryDelay = oldDelay
	})
}

func testRetryClient(endpoints ...string) *ChainClient {
	grpcEndpoints := make([]grpcEndpointConfig, 0, len(endpoints))
	for _, endpoint := range endpoints {
		grpcEndpoints = append(grpcEndpoints, grpcEndpointConfig{endpoint: endpoint})
	}
	return &ChainClient{
		grpcEndpoints:  grpcEndpoints,
		grpcTransports: []bool{false},
		protoCodec:     codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
	}
}

func TestParseGRPCEndpointsNormalizesRealEndpointsOnly(t *testing.T) {
	got, err := parseGRPCEndpoints("grpc-one.example, grpc-two.example:1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(got))
	}

	want := []grpcEndpointConfig{
		{endpoint: "grpc-one.example:9090", serverName: "grpc-one.example"},
		{endpoint: "grpc-two.example:1234", serverName: "grpc-two.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseGRPCEndpointsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: " , "},
		{name: "scheme", raw: "https://grpc.example:9090"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseGRPCEndpoints(tt.raw); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestTransportChoicesForMode(t *testing.T) {
	tests := []struct {
		name string
		mode config.GRPCTLSMode
		want []bool
	}{
		{name: "auto", mode: config.GRPCTLSModeAuto, want: []bool{true, false}},
		{name: "force TLS", mode: config.GRPCTLSModeForceTLS, want: []bool{true}},
		{name: "plaintext", mode: config.GRPCTLSModePlaintext, want: []bool{false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transportChoicesForMode(tt.mode)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithGRPCRetryRetriesBeforeSuccess(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090")
	attempts := 0

	err := c.withGRPCRetry(context.Background(), "test operation", func() error {
		attempts++
		if attempts < rpcMaxRetries {
			return errors.New("temporary failure")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != rpcMaxRetries {
		t.Fatalf("got %d attempts, want %d", attempts, rpcMaxRetries)
	}
}

func TestWithGRPCRetrySwitchesCandidates(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090", "second:9090")
	defer c.Close()
	attempts := 0

	err := c.withGRPCRetry(context.Background(), "test operation", func() error {
		attempts++
		if c.grpcEndpoints[0].endpoint == "second:9090" {
			return nil
		}
		return errors.New("first candidate failed")
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttempts := rpcMaxRetries + 1
	if attempts != wantAttempts {
		t.Fatalf("got %d attempts, want %d", attempts, wantAttempts)
	}
	if c.grpcEndpoints[0].endpoint != "second:9090" {
		t.Fatalf("got endpoint %q, want second:9090", c.grpcEndpoints[0].endpoint)
	}
}

func TestWithGRPCRetryStopsOnContextCancel(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090", "second:9090")
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	err := c.withGRPCRetry(ctx, "test operation", func() error {
		attempts++
		cancel()
		return ctx.Err()
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("got %d attempts, want 1", attempts)
	}
}

func TestGetDelegatedLicensesRetriesPaginationFromFirstPage(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090")
	var keys []string
	firstPageCalls := 0
	failSecondPageOnce := true
	c.queryClient = fakeMinerQueryClient{
		delegatedLicenses: func(_ context.Context, req *minertypes.QueryDelegatedLicensesRequest, _ ...grpc.CallOption) (*minertypes.QueryDelegatedLicensesResponse, error) {
			key := string(req.Pagination.Key)
			keys = append(keys, key)
			if key == "" {
				firstPageCalls++
				ids := []uint64{1}
				if firstPageCalls == 1 {
					ids = []uint64{99}
				}
				return &minertypes.QueryDelegatedLicensesResponse{
					LicenseIds: ids,
					Pagination: &query.PageResponse{NextKey: []byte("next")},
				}, nil
			}
			if failSecondPageOnce {
				failSecondPageOnce = false
				return nil, errors.New("second page failed")
			}
			return &minertypes.QueryDelegatedLicensesResponse{LicenseIds: []uint64{2}}, nil
		},
	}

	got, err := c.GetDelegatedLicenses(context.Background(), "operator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []uint64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if wantKeys := []string{"", "next", "", "next"}; !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("got keys %v, want %v", keys, wantKeys)
	}
}

func TestGetOwnedLicensesRetriesBalanceQuery(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090")
	balanceCalls := 0
	tokenCalls := 0
	var tokenIndex uint64
	tokenIndexSeen := false
	c.licenseClient = fakeLicenseQueryClient{
		balanceOf: func(_ context.Context, _ *licensetypes.QueryBalanceRequest, _ ...grpc.CallOption) (*licensetypes.QueryBalanceResponse, error) {
			balanceCalls++
			if balanceCalls == 1 {
				return nil, errors.New("balance failed")
			}
			return &licensetypes.QueryBalanceResponse{Balance: 1}, nil
		},
		tokenOfOwnerByIndex: func(_ context.Context, req *licensetypes.QueryTokenByOwnerIndexRequest, _ ...grpc.CallOption) (*licensetypes.QueryTokenByOwnerIndexResponse, error) {
			tokenCalls++
			tokenIndex = req.Index
			tokenIndexSeen = true
			return &licensetypes.QueryTokenByOwnerIndexResponse{Id: 42}, nil
		},
	}

	got, err := c.GetOwnedLicenses(context.Background(), "owner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []uint64{42}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if balanceCalls != 2 {
		t.Fatalf("got %d balance calls, want 2", balanceCalls)
	}
	if tokenCalls != 1 {
		t.Fatalf("got %d token calls, want 1", tokenCalls)
	}
	if !tokenIndexSeen || tokenIndex != 0 {
		t.Fatalf("got token index %d (seen=%v), want 0", tokenIndex, tokenIndexSeen)
	}
}

func TestWithGRPCRetryReturnsFinalError(t *testing.T) {
	withZeroGRPCRetryDelay(t)
	c := testRetryClient("first:9090")
	wantErr := errors.New("final failure")

	err := c.withGRPCRetry(context.Background(), "test operation", func() error {
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want wrapped final failure", err)
	}
	if !strings.Contains(err.Error(), "test operation") {
		t.Fatalf("got error %q, want operation name", err.Error())
	}
}
