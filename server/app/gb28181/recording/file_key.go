package recording

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// BuildFileKey returns the stable identity for one file on one ZLM node.
func BuildFileKey(nodeID int64, filePath string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strconv.FormatInt(nodeID, 10)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(filePath))
	return hex.EncodeToString(hash.Sum(nil))
}
