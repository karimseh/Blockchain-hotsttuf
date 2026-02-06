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

func (m *Mempool) AddTx(tx *Transaction) error {
	if err := tx.Verify(); err != nil {
		return fmt.Errorf("transaction verification failed: %w", err)
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	hash := tx.Hash(TxHasher{})
	if _, ok := m.txs[hash]; ok {
		return fmt.Errorf("transaction already in mempool")
	}

	m.txs[hash] = tx
	return nil
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
