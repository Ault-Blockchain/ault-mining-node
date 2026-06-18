package client

import (
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"

	licensetypes "github.com/Ault-Blockchain/ault/x/license/types"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

// ChainClient handles all chain interactions
type ChainClient struct {
	grpcConn          *grpc.ClientConn
	queryClient       minertypes.QueryClient
	licenseClient     licensetypes.QueryClient
	authClient        authtypes.QueryClient
	txClient          txtypes.ServiceClient
	feemarketClient   feemarkettypes.QueryClient
	grpcEndpoints     []grpcEndpointConfig
	rpcEndpoints      []string
	epochQueryConns   map[grpcEndpointConfig]*grpc.ClientConn
	epochQueryClients map[grpcEndpointConfig]minertypes.QueryClient
	chainID           string
	gasPrices         string

	// Operator key for signing
	privKey cryptotypes.PrivKey
	address sdk.AccAddress

	// signing/encoding
	interfaceRegistry codectypes.InterfaceRegistry
	protoCodec        *codec.ProtoCodec
	txConfig          client.TxConfig

	// Sequence/number management (serialize signing and keep a local next sequence)
	mu          sync.Mutex
	endpointMu  sync.RWMutex
	epochMu     sync.Mutex
	closed      bool
	accNum      uint64
	nextSeq     uint64
	seqInit     bool
	lastSeqSync time.Time
}
