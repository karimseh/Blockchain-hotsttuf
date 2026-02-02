package network

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
)

// Hash is a 32-byte SHA256 digest used throughout the consensus protocol.
type Hash [32]byte

// Hasher is a generic interface for computing hashes of typed values.
type Hasher[T any] interface {
	Hash(T) Hash
}

// ToSlice converts the fixed-size Hash array to a byte slice.
func (h Hash) ToSlice() []byte {
	b := make([]byte, 32)
	copy(b, h[:])
	return b
}

// IsZero returns true if this is the zero hash.
func (h Hash) IsZero() bool {
	return h == Hash{}
}

// CBlockHasher computes SHA256 hashes of consensus blocks.
type CBlockHasher struct{}

func (CBlockHasher) Hash(b *CBlock) Hash {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, b.ParentHash)
	buf.Write(b.Cmd)
	binary.Write(buf, binary.LittleEndian, b.Height)
	binary.Write(buf, binary.LittleEndian, b.View)
	return sha256.Sum256(buf.Bytes())
}

// QCHasher computes SHA256 hashes of quorum certificates.
// Used for vote2 signing: parties sign H(QC) to form the double certificate.
type QCHasher struct{}

func (QCHasher) Hash(qc *QC) Hash {
	buf := new(bytes.Buffer)
	if qc.Block != nil {
		blockHash := qc.Block.Hash(CBlockHasher{})
		buf.Write(blockHash[:])
	}
	binary.Write(buf, binary.LittleEndian, qc.View)
	if qc.Sig != nil {
		buf.Write(qc.Sig)
	}
	return sha256.Sum256(buf.Bytes())
}

// Hash computes the hash of a CBlock.
func (b *CBlock) Hash(hasher Hasher[*CBlock]) Hash {
	return hasher.Hash(b)
}
