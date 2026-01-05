package storage

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadKeysRoundTrip(t *testing.T) {
	dir := t.TempDir()

	opKey, _, err := GenerateOperatorKey()
	require.NoError(t, err)
	vrfKey, _, err := GenerateVRFKey()
	require.NoError(t, err)

	keys := &StoredKeys{
		OperatorKeyHex: opKey,
		VRFKeyHex:      vrfKey,
		CreatedAt:      time.Unix(1_700_000_000, 0).UTC(),
	}

	require.NoError(t, SaveKeys(dir, keys))

	loaded, err := LoadKeys(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, keys.OperatorKeyHex, loaded.OperatorKeyHex)
	assert.Equal(t, keys.VRFKeyHex, loaded.VRFKeyHex)
	assert.True(t, loaded.CreatedAt.Equal(keys.CreatedAt))
}

func TestLoadKeysMissingReturnsNil(t *testing.T) {
	dir := t.TempDir()

	keys, err := LoadKeys(dir)
	require.NoError(t, err)
	assert.Nil(t, keys)
}

func TestLoadKeysInvalidOperatorKey(t *testing.T) {
	dir := t.TempDir()

	vrfKey, _, err := GenerateVRFKey()
	require.NoError(t, err)

	payload, err := json.Marshal(&StoredKeys{
		OperatorKeyHex: "",
		VRFKeyHex:      vrfKey,
		CreatedAt:      time.Unix(1, 0).UTC(),
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(KeysPath(dir), payload, 0o600))

	_, err = LoadKeys(dir)
	require.Error(t, err)
}

func TestDeriveAddressFromKeyMatchesGenerate(t *testing.T) {
	opKey, addr, err := GenerateOperatorKey()
	require.NoError(t, err)

	derived, err := DeriveAddressFromKey(opKey)
	require.NoError(t, err)
	assert.Equal(t, addr.String(), derived.String())
}
