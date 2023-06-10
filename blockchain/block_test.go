package blockchain

import (
	"testing"
	"time"

	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
	"github.com/stretchr/testify/assert"
)

func TestBlock(t *testing.T) {
	//Tx
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	data := []byte("transaction-data")
	dest2 := types.AddressFromBytes([]byte("TesATesATesATesATesA"))
	data2 := []byte("transaction-data2")
	tx := NewTransaction(dest, data)
	tx2 := NewTransaction(dest2, data2)
	w := wallet.NewWallet()
	w2 := wallet.NewWallet()
	tx.Sign(w)
	tx2.Sign(w2)
	// Create a test header
	txHash, err := CalculateTxHash([]*Transaction{tx, tx2})
	assert.NoError(t, err, "Calculate TXs hash should not retrun err")
	header := &Header{
		TxHash:        txHash,
		PrevBlockHash: Hash{},
		BestHeight:    0,
		Timestamp:     time.Now().UnixNano(),
	}
	block := NewBlock(header, []*Transaction{tx, tx2})
	assert.NotNil(t, block, "Block should not be nil")
	block.Sign(w)
	assert.NotNil(t, block.Validator, "Block valdiator should not be nil")
	assert.NotNil(t, block.Signature, "Block signature should not be nil")
	err = block.Verify()
	assert.NoError(t, err, "Block verification should not return error")
	block2, err := NewBlockFromPreviousHeader(block.Header, []*Transaction{tx2})
	assert.NoError(t, err, "NewBlockfromprev header  should not return error")
	assert.NotNil(t, block2, "Block2 should not be nil")
	hash1 := block.Hash(BlockHasher{})
	hash2 := block.Hash(BlockHasher{})
	hash3 := block2.Hash(BlockHasher{})
	assert.NotNil(t, hash1, "hash should not be nil")
	assert.NotNil(t, hash1, "hash should not be nil")
	assert.Equal(t, hash1, hash2, "Hash1 should be the same as hash2")
	assert.NotEqual(t, hash2, hash3, "Hash2 should not be the same as hash3")

}
