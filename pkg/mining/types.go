package mining

import (
	"context"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"

	stor "github.com/Ault-Blockchain/ault-miner-node/internal/storage"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

// MinerManager manages mining for an owner with multiple licenses
type MinerManager struct {
	ownerAddr   sdk.AccAddress
	vrfPrivKey  []byte // ecvrf private key bytes
	vrfPubKey   []byte // ecvrf public key bytes (32 bytes)
	licenses    []uint64
	chainClient ChainClient
	stats       *MiningStats
	statsMu     sync.RWMutex // Protects stats.LicenseStats map
	store       *stor.DB
}

// ChainClient defines the interface for chain interactions
// This allows for dependency injection and testing
type ChainClient interface {
	GetOwnerAddress() (sdk.AccAddress, error)
	GetOwnerKeyInfo(ctx context.Context, owner string) (*minertypes.QueryOwnerKeyResponse, error)
	GetLicenseMinerInfo(ctx context.Context, licenseID uint64) (*minertypes.QueryLicenseMinerInfoResponse, error)
	GetOwnedLicenses(ctx context.Context, owner string) ([]uint64, error)
	GetDelegatedLicenses(ctx context.Context, operator string) ([]uint64, error)
	MonitorEpochs(ctx context.Context, epochChan chan<- *minertypes.QueryEpochResponse) error
	GetParams(ctx context.Context) (*minertypes.Params, error)
	BatchSubmitWork(ctx context.Context, workResults []minertypes.WorkSubmission) (string, error)
	GetCurrentEpoch(ctx context.Context) (*minertypes.QueryEpochResponse, error)
	SetOwnerVRFKey(ctx context.Context, vrfPubkey []byte, nonce uint64, autoPoP bool) error
	GetChainID() string
	Close()
}
