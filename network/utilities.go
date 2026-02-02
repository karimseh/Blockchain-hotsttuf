package network

import (
	"crypto/rand"
	"encoding/json"

	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
)

// SerializeMsg serializes a consensus Msg to JSON bytes.
func SerializeMsg(msg *Msg) ([]byte, error) {
	return json.Marshal(msg)
}

// DeserializeMsg deserializes JSON bytes into a consensus Msg.
func DeserializeMsg(data []byte) (*Msg, error) {
	msg := &Msg{}
	err := json.Unmarshal(data, msg)
	return msg, err
}

// NewRandomTx creates a random transaction for testing/demo purposes.
func NewRandomTx(w wallet.Wallet) *blockchain.Transaction {
	randBytes := make([]byte, 20)
	if _, err := rand.Read(randBytes); err != nil {
		return nil
	}
	randBytes2 := make([]byte, 50)
	if _, err := rand.Read(randBytes2); err != nil {
		return nil
	}
	dest := types.AddressFromBytes(randBytes)
	tx := blockchain.NewTransaction(dest, randBytes2)
	tx.Sign(w)
	return tx
}
