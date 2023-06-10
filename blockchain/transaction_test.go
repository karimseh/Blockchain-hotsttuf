package blockchain

import (
	"reflect"
	"testing"

	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
	"github.com/stretchr/testify/assert"
)

func TestTransaction(t *testing.T) {
	// Create a test transaction
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	data := []byte("transaction-data")
	tx := NewTransaction(dest, data)

	if reflect.TypeOf(dest.String()).Kind() != reflect.String {
		t.Errorf("Expected type string, but got %v", reflect.TypeOf(dest.String()).Kind())
	}

	assert.Equal(t, dest, tx.To, "incorrect destination address")
	assert.Equal(t, data, tx.Payload, "incorrect transaction data")
	assert.Nil(t, tx.Signature, "transaction signature should be nil initially")

	// Create a test wallet
	w := wallet.NewWallet()

	// Test signing the transaction
	err := tx.Sign(w)
	assert.NoError(t, err, "signing the transaction should not return an error")
	assert.NotNil(t, tx.Signature, "Tx Sig should not be nil")
	hash1 := tx.Hash(TxHasher{})
	assert.NotNil(t, hash1, "hash should not be nil")
	dest2 := types.AddressFromBytes([]byte("TestTestTestTestTes2"))
	data2 := []byte("transaction-data2")
	tx2 := NewTransaction(dest2, data2)
	hash2 := tx2.Hash(TxHasher{})
	assert.NotEqual(t, hash1, hash2, "hash1 should not equal hash2")
	// Test verifying the transaction
	err = tx.Verify()
	assert.NoError(t, err, "verifying the transaction should not return an error")

}
