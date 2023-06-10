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
	//adding genesis block
	bc.headers = append(bc.headers, b.Header)
	bc.blocks = append(bc.blocks, b)
	bc.blockStore[b.Hash(BlockHasher{})] = b

	for _, tx := range b.Transactions {
		bc.txStore[tx.Hash(TxHasher{})] = tx
	}
	bc.lock.Unlock()
	return nil
}

func (bc *Blockchain) Height() uint32 {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	return uint32(len(bc.headers) - 1)
}

func (bc *Blockchain) GetTxByHash(hash Hash) (*Transaction, error) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	tx, ok := bc.txStore[hash]

	if !ok {
		return nil, fmt.Errorf("transaction not found")
	}

	return tx, nil

}

func (bc *Blockchain) GetBlockByHash(hash Hash) (*Block, error) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	block, ok := bc.blockStore[hash]

	if !ok {
		return nil, fmt.Errorf("block not found")
	}

	return block, nil

}

func (bc *Blockchain) GetLastHeader() *Header {
	return bc.headers[bc.Height()]
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
