package blockchain

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

type Blockchain struct {
	blocks     []*Block
	headers    []*Header
	txStore    map[Hash]*Transaction
	blockStore map[Hash]*Block
	ScState    *SmartContract

	lock sync.RWMutex
}

func NewBlockchain(genesis *Block) *Blockchain {
	//creating bc instance
	bc := &Blockchain{
		headers:    []*Header{},
		txStore:    make(map[Hash]*Transaction),
		blockStore: make(map[Hash]*Block),
		ScState:    NewSmartContract(),
	}
	bc.Addblock(genesis)

	return bc
}

func (bc *Blockchain) Addblock(b *Block) error {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	if len(bc.headers) == 0 {
		// Genesis block: accept if PrevBlockHash is zero
		if !b.PrevBlockHash.IsZero() {
			return fmt.Errorf("genesis block must have zero PrevBlockHash")
		}
	} else {
		// Non-genesis: validate chain continuity
		lastHeader := bc.headers[len(bc.headers)-1]

		expectedPrevHash := BlockHasher{}.Hash(lastHeader)
		if b.PrevBlockHash != expectedPrevHash {
			return fmt.Errorf(
				"invalid PrevBlockHash: got %x, expected %x",
				b.PrevBlockHash, expectedPrevHash,
			)
		}

		expectedHeight := lastHeader.BestHeight + 1
		if b.BestHeight != expectedHeight {
			return fmt.Errorf(
				"invalid block height: got %d, expected %d",
				b.BestHeight, expectedHeight,
			)
		}
	}

	bc.headers = append(bc.headers, b.Header)
	bc.blocks = append(bc.blocks, b)
	bc.blockStore[b.Hash(BlockHasher{})] = b
	for _, tx := range b.Transactions {
		bc.txStore[tx.Hash(TxHasher{})] = tx
	}
	return nil
}

func (bc *Blockchain) Height() uint32 {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return uint32(len(bc.headers) - 1)
}

func (bc *Blockchain) GetTxByHash(hash Hash) (*Transaction, error) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	tx, ok := bc.txStore[hash]

	if !ok {
		return nil, fmt.Errorf("transaction not found")
	}

	return tx, nil

}

func (bc *Blockchain) GetBlockByHash(hash Hash) (*Block, error) {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	block, ok := bc.blockStore[hash]

	if !ok {
		return nil, fmt.Errorf("block not found")
	}

	return block, nil

}

func (bc *Blockchain) GetLastHeader() *Header {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return bc.headers[len(bc.headers)-1]
}

func (bc *Blockchain) String() string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	var hashes []string
	for _, block := range bc.blocks {
		hashes = append(hashes, hex.EncodeToString(block.Hash(BlockHasher{}).ToSlice()))
	}

	hashesJSON, err := json.Marshal(hashes)
	if err != nil {
		return ""
	}

	return string(hashesJSON)

}
