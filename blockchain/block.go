package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"log"
	"time"

	"github.com/karimseh/sharehr/wallet"
)

type Header struct {
	TxHash        Hash
	PrevBlockHash Hash
	BestHeight    uint32
	Timestamp     int64
}

func (h *Header) Bytes() []byte {
	buffer := &bytes.Buffer{}
	enc := gob.NewEncoder(buffer)
	enc.Encode(h)

	return buffer.Bytes()
}

type Block struct {
	*Header
	Transactions []*Transaction
	Validator    wallet.PublicKey
	Signature    *wallet.Signature
}

func NewBlock(h *Header, txs []*Transaction) *Block {
	return &Block{
		Header:       h,
		Transactions: txs,
	}
}
func NewBlockFromPreviousHeader(h *Header, txs []*Transaction) (*Block, error) {
	txHash, err := CalculateTxHash(txs)
	if err != nil {
		return nil, err
	}

	newHeader := &Header{
		TxHash:        txHash,
		PrevBlockHash: BlockHasher{}.Hash(h),
		BestHeight:    h.BestHeight + 1,
		Timestamp:     time.Now().UnixNano(),
	}

	return NewBlock(newHeader, txs), nil

}

func (b *Block) Sign(w wallet.Wallet) error {
	sig, err := w.Sign(b.Header.Bytes())
	if err != nil {
		return err
	}
	b.Validator = w.PubKey
	b.Signature = sig

	return nil
}

func (b *Block) Verify() error {
	if b.Signature == nil {
		return fmt.Errorf("block has no sig")
	}
	if !b.Signature.Verify(b.Validator, b.Header.Bytes()) {
		return fmt.Errorf("invaldid block sig")
	}

	for _, tx := range b.Transactions {
		if err := tx.Verify(); err != nil {
			return fmt.Errorf("txs not valid")
		}
	}
	root, err := CalculateTxHash(b.Transactions)
	if err != nil {
		return err
	}

	if root != b.TxHash {
		log.Print("root : ", root)
		log.Print("TxHash : ", b.TxHash)
		return fmt.Errorf("block has an invalid Tx hash")
	}
	return nil
}

func CalculateTxHash(txs []*Transaction) (hash Hash, err error) {
	buf := &bytes.Buffer{}

	for _, tx := range txs {
		if err = tx.Encode(NewGobTxEncoder(buf)); err != nil {
			return
		}
	}
	hash = sha256.Sum256(buf.Bytes())

	return
}

func (b *Block) Hash(hasher Hasher[*Header]) Hash {

	return hasher.Hash(b.Header)
}

func (b *Block) Encode(enc Encoder[*Block]) error {
	return enc.Encode(b)
}

func (b *Block) Decode(enc Decoder[*Block]) error {
	return enc.Decode(b)
}
