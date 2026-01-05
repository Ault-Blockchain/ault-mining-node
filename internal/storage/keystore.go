package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ProtonMail/go-ecvrf/ecvrf"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
)

// StoredKeys holds the auto-generated keys for fly.io deployment
type StoredKeys struct {
	OperatorKeyHex string    `json:"operator_key"`
	VRFKeyHex      string    `json:"vrf_key"`
	CreatedAt      time.Time `json:"created_at"`
}

// KeysPath returns the path to the keys file
func KeysPath(dataDir string) string {
	return filepath.Join(dataDir, "keys.json")
}

// LoadKeys loads stored keys from the data directory
// Returns nil, nil if file doesn't exist (not an error)
func LoadKeys(dataDir string) (*StoredKeys, error) {
	path := KeysPath(dataDir)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, not an error
		}
		return nil, fmt.Errorf("failed to read keys file: %w", err)
	}

	var keys StoredKeys
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse keys file: %w", err)
	}

	// Validate operator key
	if keys.OperatorKeyHex == "" {
		return nil, fmt.Errorf("keys file has empty operator_key field")
	}
	operatorKeyBytes, err := hex.DecodeString(keys.OperatorKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid operator_key hex: %w", err)
	}
	if len(operatorKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid operator_key length: expected 32 bytes, got %d", len(operatorKeyBytes))
	}

	// Validate VRF key
	if keys.VRFKeyHex == "" {
		return nil, fmt.Errorf("keys file has empty vrf_key field")
	}
	vrfKeyBytes, err := hex.DecodeString(keys.VRFKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid vrf_key hex: %w", err)
	}
	if len(vrfKeyBytes) != 64 {
		return nil, fmt.Errorf("invalid vrf_key length: expected 64 bytes, got %d", len(vrfKeyBytes))
	}

	return &keys, nil
}

// SaveKeys saves keys to the data directory with secure permissions
func SaveKeys(dataDir string, keys *StoredKeys) error {
	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keys: %w", err)
	}

	path := KeysPath(dataDir)
	// Write with restrictive permissions (owner read/write only)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write keys file: %w", err)
	}

	return nil
}

// GenerateOperatorKey generates a new secp256k1 private key for the operator wallet
// Returns the hex-encoded private key and the derived address
func GenerateOperatorKey() (privKeyHex string, address sdk.AccAddress, err error) {
	privKey, err := ethsecp256k1.GenerateKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate operator key: %w", err)
	}

	pubKey := privKey.PubKey()
	address = sdk.AccAddress(pubKey.Address())
	privKeyHex = hex.EncodeToString(privKey.Bytes())

	return privKeyHex, address, nil
}

// GenerateVRFKey generates a new Ed25519 VRF keypair
// Returns the hex-encoded private key and public key
func GenerateVRFKey() (privKeyHex, pubKeyHex string, err error) {
	privKey, err := ecvrf.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate VRF key: %w", err)
	}

	pubKey, err := privKey.Public()
	if err != nil {
		return "", "", fmt.Errorf("failed to derive VRF public key: %w", err)
	}

	privKeyHex = hex.EncodeToString(privKey.Bytes())
	pubKeyHex = hex.EncodeToString(pubKey.Bytes())

	return privKeyHex, pubKeyHex, nil
}

// DeriveAddressFromKey derives the SDK address from an operator private key hex
func DeriveAddressFromKey(privKeyHex string) (sdk.AccAddress, error) {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid operator key hex: %w", err)
	}

	privKey := &ethsecp256k1.PrivKey{Key: privKeyBytes}
	pubKey := privKey.PubKey()
	return sdk.AccAddress(pubKey.Address()), nil
}
