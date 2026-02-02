package network

import (
	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/wallet"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Transport abstracts the network and state dependencies of a Replica.
// The production implementation is *Node; tests use an in-memory mock.
type Transport interface {
	// Broadcast sends a message to all peers via pubsub.
	Broadcast(msg *Message)

	// SendMessageToPeer sends a message to a specific peer via direct stream.
	SendMessageToPeer(peerID peer.ID, msg *Message) error

	// ID returns this node's peer ID.
	ID() peer.ID

	// Peers returns all connected peer IDs (excluding self).
	Peers() peer.IDSlice

	// GetBlockchain returns the blockchain store.
	GetBlockchain() *blockchain.Blockchain

	// GetMempool returns the transaction mempool.
	GetMempool() *blockchain.Mempool

	// GetWallet returns the wallet for signing blocks.
	GetWallet() wallet.Wallet
}
