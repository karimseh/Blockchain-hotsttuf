package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
)

type Hash [32]byte
type Hasher[T any] interface {
	Hash(T) Hash
}

type TxHasher struct{}
type BlockHasher struct{}

func (h Hash) ToSlice() []byte {
	b := make([]byte, 32)
	for i := 0; i < 32; i++ {
		b[i] = h[i]
	}
	return b
}

func (TxHasher) Hash(tx *Transaction) Hash {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, tx.From)
	binary.Write(buf, binary.LittleEndian, tx.To)
	binary.Write(buf, binary.LittleEndian, tx.Payload)

	return Hash(sha256.Sum256(buf.Bytes()))
}

func (BlockHasher) Hash(h *Header) Hash {
	headerHash := sha256.Sum256(h.Bytes())
	return Hash(headerHash)
}
