package network

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
)

type Hash [32]byte
type Hasher[T any] interface {
	Hash(T) Hash
}

type NodeTHasher struct{}
type MsgHasher struct{}

func (h Hash) ToSlice() []byte {
	b := make([]byte, 32)
	for i := 0; i < 32; i++ {
		b[i] = h[i]
	}
	return b
}

func (NodeTHasher) Hash(n *NodeT) Hash {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, n.Parent)
	binary.Write(buf, binary.LittleEndian, n.Cmd)
	binary.Write(buf, binary.LittleEndian, n.Height)

	return Hash(sha256.Sum256(buf.Bytes()))
}
func (MsgHasher) Hash(m *Msg) Hash {
	buf := new(bytes.Buffer)

	binary.Write(buf, binary.LittleEndian, m.Type)
	binary.Write(buf, binary.LittleEndian, m.Node.Parent)
	binary.Write(buf, binary.LittleEndian, m.Node.Cmd)
	binary.Write(buf, binary.LittleEndian, m.Node.Height)

	return Hash(sha256.Sum256(buf.Bytes()))
}
