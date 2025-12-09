// Package mining provides core mining functionality for the Ault blockchain.
// This file contains PoW (Proof of Work) related operations.
package mining

import (
	"encoding/binary"
	"fmt"

	"github.com/zeebo/blake3"
)

const maxPoWAttempts = 1000000

// SolvePoW solves the proof-of-work puzzle
func SolvePoW(seed []byte, licenseID uint64, y []byte, kFixed uint32) ([]byte, error) {
	licenseBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(licenseBytes, licenseID)

	// Precompute base message
	baseMsg := append(seed, licenseBytes...)
	baseMsg = append(baseMsg, y...)

	// Try different nonces
	for i := 0; i < maxPoWAttempts; i++ {
		nonce := make([]byte, 8)
		binary.BigEndian.PutUint64(nonce, uint64(i))

		// Hash with nonce
		msg := append(baseMsg, nonce...)
		hash := blake3.Sum256(msg)

		// Check leading zeros
		if hasLeadingZeros(hash[:], int(kFixed)) {
			return nonce, nil
		}
	}

	return nil, fmt.Errorf("failed to solve PoW after %d attempts", maxPoWAttempts)
}

// VerifyPoW verifies a proof-of-work solution
func VerifyPoW(seed []byte, licenseID uint64, y, nonce []byte, kFixed uint32) bool {
	licenseBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(licenseBytes, licenseID)

	// Build message
	msg := append(seed, licenseBytes...)
	msg = append(msg, y...)
	msg = append(msg, nonce...)

	// Hash and check
	hash := blake3.Sum256(msg)
	return hasLeadingZeros(hash[:], int(kFixed))
}

// CountLeadingZeros counts the number of leading zero bits in a hash
func CountLeadingZeros(hash []byte) int {
	zeros := 0
	for _, b := range hash {
		if b == 0 {
			zeros += 8
		} else {
			// Count leading zeros in this byte
			for i := 7; i >= 0; i-- {
				if (b & (1 << i)) == 0 {
					zeros++
				} else {
					return zeros
				}
			}
		}
	}
	return zeros
}

// hasLeadingZeros checks if hash has required number of leading zero bits
func hasLeadingZeros(hash []byte, k int) bool {
	fullBytes := k / 8
	remainingBits := k % 8

	// Check full bytes
	for i := 0; i < fullBytes; i++ {
		if hash[i] != 0 {
			return false
		}
	}

	// Check remaining bits
	if remainingBits > 0 && fullBytes < len(hash) {
		mask := byte(0xFF << (8 - remainingBits))
		if (hash[fullBytes] & mask) != 0 {
			return false
		}
	}

	return true
}
