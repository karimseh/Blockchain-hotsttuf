package network

import (
	"fmt"
	"log"
	"sync"

	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/wallet"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestNetwork connects multiple MockTransport instances for in-memory consensus testing.
type TestNetwork struct {
	mu    sync.Mutex
	nodes map[peer.ID]*MockTransport
}

// MockTransport implements Transport for testing without libp2p.
type MockTransport struct {
	id      peer.ID
	bc      *blockchain.Blockchain
	mempool *blockchain.Mempool
	w       wallet.Wallet
	net     *TestNetwork
	replica *Replica // back-pointer for message routing
}

func (m *MockTransport) ID() peer.ID                          { return m.id }
func (m *MockTransport) GetBlockchain() *blockchain.Blockchain { return m.bc }
func (m *MockTransport) GetMempool() *blockchain.Mempool       { return m.mempool }
func (m *MockTransport) GetWallet() wallet.Wallet              { return m.w }

func (m *MockTransport) Peers() peer.IDSlice {
	m.net.mu.Lock()
	defer m.net.mu.Unlock()
	peers := make(peer.IDSlice, 0, len(m.net.nodes)-1)
	for id := range m.net.nodes {
		if id != m.id {
			peers = append(peers, id)
		}
	}
	return peers
}

func (m *MockTransport) Broadcast(msg *Message) {
	m.net.mu.Lock()
	targets := make([]*MockTransport, 0, len(m.net.nodes)-1)
	for id, node := range m.net.nodes {
		if id != m.id {
			targets = append(targets, node)
		}
	}
	m.net.mu.Unlock()

	for _, target := range targets {
		m.deliverToReplica(target, msg)
	}
}

func (m *MockTransport) SendMessageToPeer(peerID peer.ID, msg *Message) error {
	m.net.mu.Lock()
	target, ok := m.net.nodes[peerID]
	m.net.mu.Unlock()
	if !ok {
		return fmt.Errorf("peer %s not found", peerID)
	}
	m.deliverToReplica(target, msg)
	return nil
}

// deliverToReplica routes a network Message to the target's replica consensus handlers.
// Delivery is async (goroutine) to avoid deadlock since handlers acquire r.mu.
func (m *MockTransport) deliverToReplica(target *MockTransport, msg *Message) {
	if msg.Type != BlockMessage || target.replica == nil {
		return
	}
	consensusMsg, err := DeserializeMsg(msg.Payload)
	if err != nil {
		log.Printf("mock: error deserializing consensus msg: %v", err)
		return
	}
	go func() {
		switch consensusMsg.Type {
		case ProposeMsgType:
			target.replica.OnReceiveProposal(consensusMsg)
		case VoteMsgType:
			target.replica.OnReceiveVote(consensusMsg)
		case PrepareMsgType:
			target.replica.OnReceivePrepare(consensusMsg)
		case Vote2MsgType:
			target.replica.OnReceiveVote2(consensusMsg)
		case NewViewMsgType:
			target.replica.OnReceiveNewView(consensusMsg)
		}
	}()
}
