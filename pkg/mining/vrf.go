// Package mining provides core mining functionality for the Ault blockchain.
// This file contains VRF (Verifiable Random Function) related operations.
package mining

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ProtonMail/go-ecvrf/ecvrf"

	"golang.org/x/crypto/sha3"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
)

// GenerateVRFProof generates a VRF proof
func GenerateVRFProof(privKeyBytes, message []byte) (y, proof []byte, err error) {
	priv, err := ecvrf.NewPrivateKey(privKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid private key: %w", err)
	}
	vrfOutput, proofBytes, err := priv.Prove(message)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate VRF proof: %w", err)
	}
	y = vrfOutput[:32]
	proof = proofBytes
	return y, proof, nil
}

// VerifyVRFProof verifies a VRF proof (placeholder for future use)
// Currently verification is done on-chain by the miner module
func VerifyVRFProof(pubKeyBytes, message, proof []byte) ([]byte, error) {
	pubKey, err := ecvrf.NewPublicKey(pubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}
	verified, output, err := pubKey.Verify(message, proof)
	if err != nil {
		return nil, fmt.Errorf("VRF verification failed: %w", err)
	}
	if !verified {
		return nil, fmt.Errorf("VRF proof verification failed")
	}
	return output[:32], nil
}

// BuildVRFMessage builds the VRF input message with full domain separation
func BuildVRFMessage(seed []byte, licenseID uint64, owner sdk.AccAddress) []byte {
	// Domain separator
	var buf []byte
	buf = append(buf, []byte("AULT/MINER/V1")...)

	// Chain ID as u64 (numeric portion)
	chainIDNum := hashChainID(config.Get().ChainID)
	chainIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainIDBytes, chainIDNum)
	buf = append(buf, chainIDBytes...)

	// Module address: keccak256("miner")[0:20]
	mod := getModuleAddress()
	buf = append(buf, mod...)

	// Seed
	buf = append(buf, seed...)

	// License ID (u64be)
	idBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(idBytes, licenseID)
	buf = append(buf, idBytes...)

	// Owner address (20 bytes)
	buf = append(buf, owner.Bytes()...)

	// Keccak256
	hash := sha3.NewLegacyKeccak256()
	hash.Write(buf)
	return hash.Sum(nil)
}

// BuildOwnerPoP builds proof of possession message for owner VRF key with chain binding
// V2 format includes chain ID and registration epoch to prevent cross-chain replay
func BuildOwnerPoP(chainID uint64, owner, vrfPubkey []byte, registrationEpoch, nonce uint64) []byte {
	var msg []byte
	msg = append(msg, []byte("AULT/OWNER-VRF/V1")...)

	// Chain ID (8 bytes big endian)
	chainIDBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainIDBytes, chainID)
	msg = append(msg, chainIDBytes...)

	msg = append(msg, owner...)
	msg = append(msg, vrfPubkey...)

	// Registration epoch (8 bytes big endian)
	registrationBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(registrationBytes, registrationEpoch)
	msg = append(msg, registrationBytes...)

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, nonce)
	msg = append(msg, nonceBytes...)

	hash := sha3.NewLegacyKeccak256()
	hash.Write(msg)
	return hash.Sum(nil)
}

// IsBelowThreshold checks if VRF output is below threshold
func IsBelowThreshold(y, threshold []byte) bool {
	yBig := new(big.Int).SetBytes(y)
	thresholdBig := new(big.Int).SetBytes(threshold)
	return yBig.Cmp(thresholdBig) < 0
}

// hashChainID computes a unique 64-bit identifier from the full chain ID string.
// Uses Keccak256 hash of the entire chain ID to match chain-side VRF message construction.
func hashChainID(chainID string) uint64 {
	if chainID == "" {
		return 1
	}
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(chainID))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// getModuleAddress derives the 20-byte module address constant used on-chain
func getModuleAddress() []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte("miner"))
	sum := h.Sum(nil)
	return sum[:20]
}
