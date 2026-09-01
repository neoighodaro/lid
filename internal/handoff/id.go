package handoff

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"time"
)

func NewID() string {
	var buf [12]byte
	binary.BigEndian.PutUint64(buf[0:8], uint64(time.Now().UTC().UnixNano()))
	_, _ = rand.Read(buf[8:12])
	return hex.EncodeToString(buf[:])
}
