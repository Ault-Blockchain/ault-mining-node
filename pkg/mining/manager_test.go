package mining

import (
	"crypto/rand"
	"encoding/binary"
	"os"
	"testing"

	"github.com/ProtonMail/go-ecvrf/ecvrf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"

	"golang.org/x/crypto/sha3"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	"github.com/Ault-Blockchain/ault/x/miner/keeper"
)

func TestMain(m *testing.M) {
	// Set up environment for tests
	os.Setenv("CHAIN_ID", "cosmos_262144-1")
	config.Load()
	os.Exit(m.Run())
}

func TestBuildOwnerPoP(t *testing.T) {
	owner := sdk.AccAddress([]byte("test_owner__________"))
	vrfPubkey := make([]byte, 32)
	nonce := uint64(42)

	// Fill with test data
	for i := range vrfPubkey {
		vrfPubkey[i] = byte(i)
	}

	chainID := uint64(262144) // test chain ID
	registrationEpoch := uint64(0)
	popMsg := BuildOwnerPoP(chainID, owner.Bytes(), vrfPubkey, registrationEpoch, nonce)

	// Verify the message structure
	assert.NotNil(t, popMsg)
	assert.Len(t, popMsg, 32) // Keccak256 output

	// Build expected message with V2 format
	var expected []byte
	expected = append(expected, []byte("AULT/OWNER-VRF/V1")...)

	chainIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainIDBytes, chainID)
	expected = append(expected, chainIDBytes...)

	expected = append(expected, owner.Bytes()...)
	expected = append(expected, vrfPubkey...)

	registrationBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(registrationBytes, registrationEpoch)
	expected = append(expected, registrationBytes...)

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	expected = append(expected, nonceBytes...)

	// Hash and compare
	hash := sha3.NewLegacyKeccak256()
	hash.Write(expected)
	expectedHash := hash.Sum(nil)

	assert.Equal(t, expectedHash, popMsg)
}

func TestBuildVRFMessage(t *testing.T) {
	seed := make([]byte, 32)
	licenseID := uint64(123)
	owner := sdk.AccAddress([]byte("test_owner__________"))

	// Fill seed with test data
	for i := range seed {
		seed[i] = byte(i * 3)
	}

	// Chain ID is set in TestMain via config.Load()
	msg := BuildVRFMessage(seed, licenseID, owner)

	// Verify output
	assert.NotNil(t, msg)
	assert.Len(t, msg, 32) // Keccak256 output
}

func TestClientBindingMatchesKeeper(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	licenseID := uint64(7)
	owner := sdk.AccAddress([]byte("owner_addr____________"))

	// Chain ID is set in TestMain via config.Load()

	// Client-computed message
	clientMsg := BuildVRFMessage(seed, licenseID, owner)

	// Keeper-computed message (needs a context with ChainID)
	ctx := sdk.NewContext(nil, tmproto.Header{ChainID: "cosmos_262144-1"}, false, log.NewNopLogger())
	keeperMsg := keeper.BuildVRFMessage(ctx, seed, licenseID, owner)

	assert.Equal(t, keeperMsg, clientMsg)
}

func TestGenerateVRFProof(t *testing.T) {
	// Generate test ECVRF key
	priv, err := ecvrf.GenerateKey(rand.Reader)
	require.NoError(t, err)

	// Generate proof
	message := make([]byte, 32)
	rand.Read(message)

	y, proof, err := GenerateVRFProof(priv.Bytes(), message)
	require.NoError(t, err)

	// Verify outputs
	assert.Len(t, y, 32)
	assert.Len(t, proof, 80) // ECVRF proof size

	// Verify deterministic
	y2, proof2, err := GenerateVRFProof(priv.Bytes(), message)
	require.NoError(t, err)
	assert.Equal(t, y, y2)
	assert.Equal(t, proof, proof2)
}

func TestIsBelowThreshold(t *testing.T) {
	tests := []struct {
		name      string
		y         []byte
		threshold []byte
		expected  bool
	}{
		{
			name:      "y below threshold",
			y:         []byte{0x00, 0x01},
			threshold: []byte{0x00, 0x02},
			expected:  true,
		},
		{
			name:      "y equal to threshold",
			y:         []byte{0x00, 0x02},
			threshold: []byte{0x00, 0x02},
			expected:  false,
		},
		{
			name:      "y above threshold",
			y:         []byte{0x00, 0x03},
			threshold: []byte{0x00, 0x02},
			expected:  false,
		},
		{
			name:      "longer values",
			y:         make([]byte, 32),
			threshold: []byte{0xFF, 0xFF, 0xFF, 0xFF},
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBelowThreshold(tt.y, tt.threshold)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSolvePoW(t *testing.T) {
	seed := make([]byte, 32)
	licenseID := uint64(1)
	y := make([]byte, 32)

	// Test with low difficulty
	kFixed := uint32(8) // 8 leading zero bits (1 byte)

	nonce, err := SolvePoW(seed, licenseID, y, kFixed)
	require.NoError(t, err)
	assert.Len(t, nonce, 8)

	// Verify the solution
	licenseBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(licenseBytes, licenseID)
	msg := append(seed, licenseBytes...)
	msg = append(msg, y...)
	msg = append(msg, nonce...)

	hash := blake3.Sum256(msg)
	assert.True(t, hasLeadingZeros(hash[:], int(kFixed)))
}

func TestHasLeadingZeros(t *testing.T) {
	tests := []struct {
		name     string
		hash     []byte
		k        int
		expected bool
	}{
		{
			name:     "8 leading zeros (1 byte)",
			hash:     []byte{0x00, 0xFF, 0xFF},
			k:        8,
			expected: true,
		},
		{
			name:     "16 leading zeros (2 bytes)",
			hash:     []byte{0x00, 0x00, 0xFF},
			k:        16,
			expected: true,
		},
		{
			name:     "4 leading zeros",
			hash:     []byte{0x0F, 0xFF},
			k:        4,
			expected: true,
		},
		{
			name:     "not enough zeros",
			hash:     []byte{0x01, 0x00},
			k:        8,
			expected: false,
		},
		{
			name:     "12 leading zeros (1.5 bytes)",
			hash:     []byte{0x00, 0x0F, 0xFF},
			k:        12,
			expected: true,
		},
		{
			name:     "12 leading zeros fail",
			hash:     []byte{0x00, 0x1F, 0xFF},
			k:        12,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasLeadingZeros(tt.hash, tt.k)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkVRFProof(b *testing.B) {
	priv, _ := ecvrf.GenerateKey(rand.Reader)
	message := make([]byte, 32)
	rand.Read(message)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateVRFProof(priv.Bytes(), message)
	}
}

func BenchmarkPoW(b *testing.B) {
	seed := make([]byte, 32)
	licenseID := uint64(1)
	y := make([]byte, 32)
	kFixed := uint32(16) // 16 leading zeros

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SolvePoW(seed, licenseID, y, kFixed)
	}
}
