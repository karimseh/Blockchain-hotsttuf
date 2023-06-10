package blockchain

import (
	"fmt"
	"sync"
)

type Mempool struct {
	txs  map[Hash]*Transaction
	lock sync.RWMutex
}

func NewMempool() *Mempool {
	return &Mempool{
		txs: make(map[Hash]*Transaction),
	}
}

func (m *Mempool) AddTx(tx *Transaction) {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, ok := m.txs[tx.Hash(TxHasher{})]
	if ok {
		fmt.Print("tx already in mempool")
		return
	}

	m.txs[tx.Hash(TxHasher{})] = tx
}

func (m *Mempool) DeleteTx(tx *Transaction) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.txs, tx.Hash(TxHasher{}))
}

func (m *Mempool) GetTxs() []*Transaction {
	m.lock.RLock()
	defer m.lock.RUnlock()
	txs := []*Transaction{}
	for _, tx := range m.txs {
		txs = append(txs, tx)
	}
	return txs
}

func (m *Mempool) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.txs)
}
