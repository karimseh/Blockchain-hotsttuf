package blockchain

import (
	"sync"
	"testing"
	"time"

	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers ---

func newGenesisBlock() *Block {
	h := &Header{
		TxHash:        Hash{},
		PrevBlockHash: Hash{},
		BestHeight:    1,
		Timestamp:     0,
	}
	return NewBlock(h, []*Transaction{})
}

func newSignedBlockFromPrev(t *testing.T, prev *Header, w wallet.Wallet) *Block {
	t.Helper()
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("test-data"))
	require.NoError(t, tx.Sign(w))

	b, err := NewBlockFromPreviousHeader(prev, []*Transaction{tx})
	require.NoError(t, err)
	require.NoError(t, b.Sign(w))
	return b
}

// --- Addblock Chain Validation ---

func TestAddblock_GenesisAccepted(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	assert.Equal(t, uint32(0), bc.Height())
}

func TestAddblock_ValidBlockAccepted(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	block := newSignedBlockFromPrev(t, bc.GetLastHeader(), w)
	err := bc.Addblock(block)
	assert.NoError(t, err)
	assert.Equal(t, uint32(1), bc.Height())
}

func TestAddblock_WrongPrevHash_Rejected(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("data"))
	require.NoError(t, tx.Sign(w))

	txHash, err := CalculateTxHash([]*Transaction{tx})
	require.NoError(t, err)

	badBlock := NewBlock(&Header{
		TxHash:        txHash,
		PrevBlockHash: Hash{0x01}, // wrong
		BestHeight:    2,
		Timestamp:     time.Now().UnixNano(),
	}, []*Transaction{tx})
	require.NoError(t, badBlock.Sign(w))

	err = bc.Addblock(badBlock)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PrevBlockHash")
	assert.Equal(t, uint32(0), bc.Height()) // chain unchanged
}

func TestAddblock_WrongHeight_Rejected(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	lastHeader := bc.GetLastHeader()
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("data"))
	require.NoError(t, tx.Sign(w))

	txHash, err := CalculateTxHash([]*Transaction{tx})
	require.NoError(t, err)

	badBlock := NewBlock(&Header{
		TxHash:        txHash,
		PrevBlockHash: BlockHasher{}.Hash(lastHeader), // correct hash
		BestHeight:    99,                              // wrong height
		Timestamp:     time.Now().UnixNano(),
	}, []*Transaction{tx})
	require.NoError(t, badBlock.Sign(w))

	err = bc.Addblock(badBlock)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid block height")
}

func TestAddblock_MultipleBlocks_ChainGrows(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	for i := 0; i < 5; i++ {
		block := newSignedBlockFromPrev(t, bc.GetLastHeader(), w)
		err := bc.Addblock(block)
		require.NoError(t, err)
	}
	assert.Equal(t, uint32(5), bc.Height())
}

// --- GetLastHeader ---

func TestGetLastHeader_ReturnsLastBlock(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	header := bc.GetLastHeader()
	assert.Equal(t, uint32(1), header.BestHeight)

	block := newSignedBlockFromPrev(t, header, w)
	require.NoError(t, bc.Addblock(block))

	header2 := bc.GetLastHeader()
	assert.Equal(t, uint32(2), header2.BestHeight)
}

// --- Read Lock (concurrent readers shouldn't deadlock) ---

func TestGetTxByHash_ConcurrentReads(t *testing.T) {
	genesis := newGenesisBlock()
	bc := NewBlockchain(genesis)
	w := wallet.NewWallet()

	block := newSignedBlockFromPrev(t, bc.GetLastHeader(), w)
	require.NoError(t, bc.Addblock(block))

	txHash := block.Transactions[0].Hash(TxHasher{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := bc.GetTxByHash(txHash)
			assert.NoError(t, err)
			assert.NotNil(t, tx)
		}()
	}
	wg.Wait()
}

// --- Mempool Verification ---

func TestMempool_AddTx_ValidTx_Accepted(t *testing.T) {
	mp := NewMempool()
	w := wallet.NewWallet()
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("data"))
	require.NoError(t, tx.Sign(w))

	err := mp.AddTx(tx)
	assert.NoError(t, err)
	assert.Equal(t, 1, mp.Len())
}

func TestMempool_AddTx_UnsignedTx_Rejected(t *testing.T) {
	mp := NewMempool()
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("data"))

	err := mp.AddTx(tx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verification failed")
	assert.Equal(t, 0, mp.Len())
}

func TestMempool_AddTx_DuplicateTx_Rejected(t *testing.T) {
	mp := NewMempool()
	w := wallet.NewWallet()
	dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
	tx := NewTransaction(dest, []byte("data"))
	require.NoError(t, tx.Sign(w))

	err := mp.AddTx(tx)
	assert.NoError(t, err)

	err = mp.AddTx(tx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already in mempool")
	assert.Equal(t, 1, mp.Len())
}

// --- Hash.IsZero ---

func TestHash_IsZero(t *testing.T) {
	var zeroHash Hash
	assert.True(t, zeroHash.IsZero())

	nonZero := Hash{0x01}
	assert.False(t, nonZero.IsZero())
}
