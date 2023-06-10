package blockchain

import (
	"fmt"

	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
)

type Transaction struct {
	From      wallet.PublicKey
	To        types.Address
	Payload   []byte
	Signature *wallet.Signature
	TxHash    Hash
}

func NewTransaction(dest types.Address, data []byte) *Transaction {
	return &Transaction{
		To:      dest,
		Payload: data,
	}
}

func (tx *Transaction) Hash(hasher Hasher[*Transaction]) Hash {
	return hasher.Hash(tx)
}

func (tx *Transaction) Sign(w wallet.Wallet) error {
	hash := tx.Hash(TxHasher{})
	sig, err := w.Sign(hash.ToSlice())
	if err != nil {
		return err
	}
	tx.From = w.PubKey
	tx.Signature = sig
	tx.TxHash = hash
	return nil
}

func (tx *Transaction) Verify() error {
	if tx.Signature == nil {
		return fmt.Errorf("tx has no sig")
	}
	if !tx.Signature.Verify(tx.From, tx.TxHash[:]) {
		return fmt.Errorf("invalid tx sig")
	}
	return nil
}

func (tx *Transaction) Decode(dec Decoder[*Transaction]) error {
	return dec.Decode(tx)
}

func (tx *Transaction) Encode(enc Encoder[*Transaction]) error {
	return enc.Encode(tx)
}
