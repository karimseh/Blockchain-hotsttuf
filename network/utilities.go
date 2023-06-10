package network

import (
	"crypto/rand"
	"encoding/json"

	"github.com/coinbase/kryptology/pkg/ted25519/ted25519"
	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
)

type MsgType int

const (
	Generic MsgType = iota
	NewView
)

type NodeT struct {
	Parent  Hash
	Cmd     []byte
	Justify QC
	Height  uint32
}

type QC struct {
	Type MsgType
	Node *NodeT
	Sig  ted25519.Signature
}
type Msg struct {
	Type       MsgType
	Node       NodeT
	Justify    QC
	PartialSig ted25519.PartialSignature
}

func NewMsg(typ MsgType, node NodeT, qc QC) Msg {
	return Msg{
		Type:    typ,
		Node:    node,
		Justify: qc,
	}
}

func (m *Msg) Hash(hasher Hasher[*Msg]) Hash {
	return hasher.Hash(m)
}
func (n *NodeT) Hash(hasher Hasher[*NodeT]) Hash {
	return hasher.Hash(n)
}
func (n *NodeT) Extends(b *NodeT) bool {
	stack := []*NodeT{n}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Parent == b.Hash(NodeTHasher{}) {
			return true
		}

		if node.Justify.Node != nil {
			stack = append(stack, node.Justify.Node)
		}
	}

	return false
}
func NewQC(votes []*Msg) QC {
	partialSigs := []*ted25519.PartialSignature{}
	for _, vote := range votes {
		partialSigs = append(partialSigs, &vote.PartialSig)
	}
	sig, _ := ted25519.Aggregate(partialSigs, &ted25519.ShareConfiguration{T: 2, N: 4})
	qc := QC{
		Type: votes[0].Type,
		Node: &votes[0].Node,
		Sig:  sig,
	}
	return qc
}

// func getIndexByValue(slice interface{}, value interface{}) int {
// 	sliceValue := reflect.ValueOf(slice)
// 	valueValue := reflect.ValueOf(value)

// 	for i := 0; i < sliceValue.Len(); i++ {
// 		element := sliceValue.Index(i)
// 		if reflect.DeepEqual(element.Interface(), valueValue.Interface()) {
// 			return i
// 		}
// 	}

//		return -1 // Return -1 if value is not found in the slice
//	}
func contains(s []*Msg, str *Msg) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func NewRandomTx(w wallet.Wallet) *blockchain.Transaction {
	randBytes := make([]byte, 20)
	_, err := rand.Read(randBytes)
	if err != nil {
		return nil
	}
	randBytes2 := make([]byte, 50)
	_, err = rand.Read(randBytes2)
	if err != nil {
		return nil
	}
	//randBytes, _:= rand.Read(make([]byte, 20))
	dest := types.AddressFromBytes(randBytes)
	data := randBytes2
	tx := blockchain.NewTransaction(dest, data)
	tx.Sign(w)

	return tx

}

func SerializeMsg(msg *Msg) ([]byte, error) {
	return json.Marshal(msg)
}

func DeserializeMsg(data []byte) (*Msg, error) {
	msg := &Msg{}
	err := json.Unmarshal(data, msg)
	return msg, err
}
