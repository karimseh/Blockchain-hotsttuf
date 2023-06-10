package blockchain

import (
	"github.com/karimseh/sharehr/types"
	"github.com/karimseh/sharehr/wallet"
)

type State struct {
	value int
}

type SmartContract struct {
	Address types.Address
	State   State
}

func NewSmartContract() *SmartContract {
	w := wallet.NewWallet()
	return &SmartContract{
		Address: w.Address(),
		State:   State{value: 0},
	}
}

// smart contract functions
func (sc *SmartContract) Add(v int) {
	sc.State.value = sc.State.value + v
}
