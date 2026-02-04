// pacemaker.go implements view management for HotStuff-2 (Figure 2 from the paper).
//
// The pacemaker is responsible for:
//   - Deterministic leader election (round-robin over sorted peer list)
//   - View advancement via double certificates (responsive) or timeouts
//   - Starting consensus once enough peers are connected
//
// Per the paper, view advancement happens in two cases:
//   Case 1: Receiving C_{v'-1}(C_{v'-1}(B_{k'})) — double cert, responsive
//   Case 2: Timeout expires, send NewView to next leader — waits O(Δ)
package network

import (
	"log"
	"sort"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// isLeader returns true if this node is the leader for view v.
// Leader election is deterministic: leader(v) = sortedPeers[v mod n].
func (r *Replica) isLeader(v View) bool {
	return r.getLeader(v) == r.network.ID()
}

// getLeader returns the peer ID of the leader for view v.
func (r *Replica) getLeader(v View) peer.ID {
	peers := r.getSortedPeers()
	if len(peers) == 0 {
		return r.network.ID()
	}
	idx := int(v) % len(peers)
	return peers[idx]
}

// getSortedPeers returns a deterministic, sorted list of all peers including self.
func (r *Replica) getSortedPeers() peer.IDSlice {
	peerList := append(r.network.Peers(), r.network.ID())

	// Deduplicate
	seen := make(map[peer.ID]bool)
	unique := make(peer.IDSlice, 0, len(peerList))
	for _, p := range peerList {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	sort.Slice(unique, unique.Less)
	return unique
}

// StartConsensus begins the consensus protocol at view 1.
// Called once enough peers are connected (n = 3t+1).
func (r *Replica) StartConsensus() {
	log.Printf("Starting HotStuff-2 consensus with %d parties (t=%d, quorum=%d)",
		r.n, r.t, r.quorum)

	peers := r.getSortedPeers()
	log.Printf("Peers: %v", peers)
	log.Printf("Leader for view 1: %s (me: %s)", r.getLeader(1), r.network.ID())

	// Enter view 1 without a double cert (Case 2)
	r.EnterView(1, nil)
}

// WaitForPeersAndStart polls until n peers are connected, then starts consensus.
func (r *Replica) WaitForPeersAndStart() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		peers := r.getSortedPeers()
		if len(peers) >= r.n {
			log.Printf("All %d peers connected, starting consensus", len(peers))
			r.StartConsensus()
			return
		}
		log.Printf("Waiting for peers: %d/%d connected", len(peers), r.n)
	}
}
