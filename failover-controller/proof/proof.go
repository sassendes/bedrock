package proof

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Signed is what postgres returns from bedrock.bedrock_prove. Only postgres holds the key that makes MAC.
type Signed struct {
	KeyID      string `json:"keyId"`
	Challenge  string `json:"challenge"`
	Observer   string `json:"observer"`
	Instance   string `json:"instance"`
	SystemID   string `json:"systemId"`
	Timeline   int64  `json:"timeline"`
	LSN        string `json:"lsn"`
	InRecovery bool   `json:"inRecovery"`
	AtMs       int64  `json:"atMs"`
	MAC        string `json:"mac"`
}

// Payload must match concat_ws in bedrock.bedrock_prove byte for byte.
func (s Signed) Payload() string {
	return strings.Join([]string{
		s.KeyID, s.Challenge, s.Observer, s.Instance, s.SystemID,
		strconv.FormatInt(s.Timeline, 10), s.LSN, strconv.FormatBool(s.InRecovery), strconv.FormatInt(s.AtMs, 10),
	}, "|")
}

type Keys map[string][]byte

var ErrUnknownKey = errors.New("unknown key id")

func (k Keys) Verify(s Signed) error {
	key, ok := k[s.KeyID]
	if !ok {
		return ErrUnknownKey
	}
	got, err := hex.DecodeString(s.MAC)
	if err != nil {
		return fmt.Errorf("mac is not hex: %w", err)
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s.Payload()))
	if !hmac.Equal(got, m.Sum(nil)) {
		return errors.New("mac does not match")
	}
	return nil
}

// LoadKeys reads one hex key per file, the file name being the key id, so a rotation is one more file.
func LoadKeys(dir string) (Keys, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	keys := Keys{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(raw) < 32 {
			return nil, fmt.Errorf("key %s must be at least 32 bytes of hex", e.Name())
		}
		keys[e.Name()] = raw
	}
	if len(keys) == 0 {
		return nil, errors.New("no proof keys in " + dir)
	}
	return keys, nil
}
