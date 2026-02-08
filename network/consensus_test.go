package network

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/coinbase/kryptology/pkg/ted25519/ted25519"
	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/wallet"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test Helpers ---

// setupTestNetwork creates a 4-node in-memory test network with threshold key shares.
func setupTestNetwork(t *testing.T) (*TestNetwork, []*Replica) {
	t.Helper()

	keyShares := NewKeyShareMap()

	// Create deterministic peer IDs
	peerIDs := make([]peer.ID, NumParties)
	for i := 0; i < NumParties; i++ {
		peerIDs[i] = peer.ID(fmt.Sprintf("replica-%d", i))
	}
	sort.Slice(peerIDs, func(i, j int) bool {
		return peerIDs[i] < peerIDs[j]
	})

	net := &TestNetwork{nodes: make(map[peer.ID]*MockTransport)}
	replicas := make([]*Replica, NumParties)

	for i := 0; i < NumParties; i++ {
		genesis := blockchain.NewBlock(&blockchain.Header{
			BestHeight: 1,
		}, []*blockchain.Transaction{})
		bc := blockchain.NewBlockchain(genesis)
		mp := blockchain.NewMempool()
		w := wallet.NewWallet()

		mt := &MockTransport{
			id:      peerIDs[i],
			bc:      bc,
			mempool: mp,
			w:       w,
			net:     net,
		}
		net.nodes[peerIDs[i]] = mt

		ks := keyShares[i]
		replicas[i] = newReplica(&ks, mt, NumParties, FaultyThreshold)
		replicas[i].delta = 10 * time.Millisecond // fast for tests
		mt.replica = replicas[i]
	}

	return net, replicas
}

// seedMempool adds a random transaction to each replica's mempool.
func seedMempool(t *testing.T, replicas []*Replica) {
	t.Helper()
	for _, r := range replicas {
		tx := NewRandomTx(r.network.GetWallet())
		err := r.network.GetMempool().AddTx(tx)
		require.NoError(t, err, "seeding mempool should not fail with valid tx")
	}
}

// waitForCondition polls until cond returns true or timeout expires.
func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for: %s", desc)
		case <-ticker.C:
			if cond() {
				return
			}
		}
	}
}

// getExecHeight reads the execHeight safely.
func getExecHeight(r *Replica) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.execHeight
}

// getCurrentView reads the currentView safely.
func getCurrentView(r *Replica) View {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentView
}

// --- Test Cases ---

// TestKeyShareGeneration verifies that threshold signature generation, partial
// signing, aggregation, and verification all work correctly with the 3-of-4 scheme.
func TestKeyShareGeneration(t *testing.T) {
	shares := NewKeyShareMap()
	assert.Equal(t, NumParties, len(shares))

	data := []byte("test-block-hash-for-signing")

	// Sign with 3 shares (quorum)
	partials := make([]*ted25519.PartialSignature, 0, Quorum)
	for i := 0; i < Quorum; i++ {
		ks := shares[i]
		ps := signWithKeyShare(&ks, data)
		partials = append(partials, ps)
	}

	sig, err := ted25519Aggregate(partials, Quorum, NumParties)
	require.NoError(t, err)
	assert.NotNil(t, sig)

	ok, verifyErr := ted25519Verify(shares[0].Pub, data, sig)
	require.NoError(t, verifyErr)
	assert.True(t, ok, "aggregated threshold signature should verify")
}

// TestSingleViewConsensus tests the full happy-path flow for one view:
// Propose → Vote → QC → Prepare → Vote2 → Double Cert
func TestSingleViewConsensus(t *testing.T) {
	_, replicas := setupTestNetwork(t)
	seedMempool(t, replicas)

	// Start all replicas at view 1 (Case 2: no double cert)
	for _, r := range replicas {
		go r.EnterView(1, nil)
	}

	// The leader proposes, parties vote, leader forms QC, broadcasts prepare,
	// parties send vote2 to next leader → next leader forms double cert.
	// The commit happens when the next leader proposes with the double cert.

	// Wait for at least view 2 (double cert triggers view advance)
	waitForCondition(t, 3*time.Second, "any replica reaches view >= 2", func() bool {
		for _, r := range replicas {
			if getCurrentView(r) >= 2 {
				return true
			}
		}
		return false
	})

	// The leader of view 2 should have the double cert from view 1.
	// When it proposes in view 2, the double cert is included, and receivers commit.
	// So we need view 2's proposal to be received for the commit to happen.
	// Seed more txs for view 2's block.
	seedMempool(t, replicas)

	waitForCondition(t, 5*time.Second, "at least one replica commits height 1", func() bool {
		for _, r := range replicas {
			if getExecHeight(r) >= 1 {
				return true
			}
		}
		return false
	})

	// Verify at least one replica committed
	committed := 0
	for _, r := range replicas {
		if getExecHeight(r) >= 1 {
			committed++
		}
	}
	assert.Greater(t, committed, 0, "at least one replica should have committed")
	t.Logf("Replicas that committed: %d/%d", committed, len(replicas))
}

// TestQCFormation verifies that the leader correctly aggregates votes into a QC
// and broadcasts a prepare message.
func TestQCFormation(t *testing.T) {
	_, replicas := setupTestNetwork(t)

	// Find the leader for view 1
	leaderIdx := -1
	for i, r := range replicas {
		if r.isLeader(1) {
			leaderIdx = i
			break
		}
	}
	require.NotEqual(t, -1, leaderIdx, "must find a leader for view 1")
	leader := replicas[leaderIdx]

	leader.mu.Lock()
	leader.currentView = 1
	leader.mu.Unlock()

	// Create a test block
	block := &CBlock{
		ParentHash: Hash{},
		Cmd:        []byte("test-cmd"),
		Height:     1,
		View:       1,
	}
	blockHash := block.Hash(CBlockHasher{})

	// Send quorum (3) votes from non-leader replicas
	voterCount := 0
	for i, r := range replicas {
		if i == leaderIdx || voterCount >= Quorum {
			continue
		}
		ps := r.sign(blockHash.ToSlice())
		voteMsg := &Msg{
			Type:       VoteMsgType,
			View:       1,
			BlockHash:  blockHash,
			Block:      block,
			PartialSig: ps,
		}
		leader.OnReceiveVote(voteMsg)
		voterCount++
	}

	// Verify QC was formed
	leader.mu.Lock()
	highView := leader.highQC.View
	leader.mu.Unlock()

	assert.Equal(t, View(1), highView, "leader should have formed QC at view 1")
}

// TestSafetyLockingRule verifies that a replica rejects proposals whose QC
// is ranked lower than its locked certificate.
func TestSafetyLockingRule(t *testing.T) {
	_, replicas := setupTestNetwork(t)

	r0 := replicas[0]

	// Manually set a high locked QC on replica 0
	r0.mu.Lock()
	r0.currentView = 5
	r0.lockedQC = &QC{
		Block: &CBlock{Height: 10, View: 4},
		View:  4,
	}
	r0.mu.Unlock()

	// Send a proposal with a lower-ranked QC (view 2 < locked view 4)
	badProposal := &Msg{
		Type: ProposeMsgType,
		View: 5,
		Block: &CBlock{
			ParentHash: Hash{},
			Cmd:        []byte("bad-block"),
			Height:     11,
			View:       5,
		},
		HighQC: &QC{
			Block: &CBlock{Height: 5, View: 2},
			View:  2,
		},
	}
	r0.OnReceiveProposal(badProposal)

	// Verify replica did NOT vote
	r0.mu.Lock()
	voted := r0.votedInView
	r0.mu.Unlock()

	assert.NotEqual(t, View(5), voted,
		"replica should reject proposal with QC ranked lower than locked cert")
}

// TestSafetyAcceptsHigherQC verifies that a replica DOES vote when the
// proposal's QC is ranked higher than its locked certificate.
func TestSafetyAcceptsHigherQC(t *testing.T) {
	_, replicas := setupTestNetwork(t)

	r0 := replicas[0]

	r0.mu.Lock()
	r0.currentView = 5
	r0.lockedQC = &QC{
		Block: &CBlock{Height: 3, View: 2},
		View:  2,
	}
	r0.mu.Unlock()

	// Send a proposal with a higher-ranked QC (view 4 > locked view 2)
	goodProposal := &Msg{
		Type: ProposeMsgType,
		View: 5,
		Block: &CBlock{
			ParentHash: Hash{},
			Cmd:        []byte("good-block"),
			Height:     11,
			View:       5,
		},
		HighQC: &QC{
			Block: &CBlock{Height: 10, View: 4},
			View:  4,
		},
	}
	r0.OnReceiveProposal(goodProposal)

	r0.mu.Lock()
	voted := r0.votedInView
	r0.mu.Unlock()

	assert.Equal(t, View(5), voted,
		"replica should accept proposal with QC ranked higher than locked cert")
}

// TestViewTimeout verifies that replicas advance to the next view when
// the current view times out (e.g., leader crash).
func TestViewTimeout(t *testing.T) {
	_, replicas := setupTestNetwork(t)

	// Set very short delta for fast timeout
	for _, r := range replicas {
		r.delta = 5 * time.Millisecond
	}

	// Find leader for view 1
	var leaderID peer.ID
	for _, r := range replicas {
		if r.isLeader(1) {
			leaderID = r.network.ID()
			break
		}
	}

	// Start only non-leader replicas (simulate leader crash)
	for _, r := range replicas {
		if r.network.ID() != leaderID {
			go r.EnterView(1, nil)
		}
	}

	// Wait for view timeout (6*delta = 30ms) + margin
	waitForCondition(t, 500*time.Millisecond, "non-leader replicas advance to view >= 2", func() bool {
		advanced := 0
		for _, r := range replicas {
			if r.network.ID() == leaderID {
				continue
			}
			if getCurrentView(r) >= 2 {
				advanced++
			}
		}
		return advanced >= NumParties-1 // all non-leaders
	})

	// Verify all non-leader replicas advanced
	for _, r := range replicas {
		if r.network.ID() == leaderID {
			continue
		}
		v := getCurrentView(r)
		assert.GreaterOrEqual(t, v, View(2),
			"non-leader replica should have timed out to view >= 2")
	}
}

// TestDuplicateVoteRejection verifies that the leader ignores duplicate votes
// from the same share identifier.
func TestDuplicateVoteRejection(t *testing.T) {
	_, replicas := setupTestNetwork(t)

	leaderIdx := -1
	for i, r := range replicas {
		if r.isLeader(1) {
			leaderIdx = i
			break
		}
	}
	require.NotEqual(t, -1, leaderIdx)
	leader := replicas[leaderIdx]

	leader.mu.Lock()
	leader.currentView = 1
	leader.mu.Unlock()

	block := &CBlock{
		ParentHash: Hash{},
		Cmd:        []byte("test"),
		Height:     1,
		View:       1,
	}
	blockHash := block.Hash(CBlockHasher{})

	// Pick one non-leader voter
	voterIdx := (leaderIdx + 1) % NumParties
	voter := replicas[voterIdx]
	ps := voter.sign(blockHash.ToSlice())

	voteMsg := &Msg{
		Type:       VoteMsgType,
		View:       1,
		BlockHash:  blockHash,
		Block:      block,
		PartialSig: ps,
	}

	// Send the same vote 3 times
	leader.OnReceiveVote(voteMsg)
	leader.OnReceiveVote(voteMsg)
	leader.OnReceiveVote(voteMsg)

	// Should only have 1 vote counted
	leader.mu.Lock()
	count := len(leader.votes[blockHash])
	leader.mu.Unlock()

	assert.Equal(t, 1, count, "duplicate votes should be rejected")
}

// --- Threshold signature helpers for TestKeyShareGeneration ---
// These wrap the ted25519 calls to keep the test clean.

func signWithKeyShare(ks *KeyShare, data []byte) *ted25519.PartialSignature {
	return ted25519.TSign(data, ks.Key, ks.Pub, ks.Nonce, ks.NoncePub)
}

func ted25519Aggregate(partials []*ted25519.PartialSignature, quorum, numParties int) (ted25519.Signature, error) {
	return ted25519.Aggregate(partials, &ted25519.ShareConfiguration{T: quorum, N: numParties})
}

func ted25519Verify(pub ted25519.PublicKey, data []byte, sig ted25519.Signature) (bool, error) {
	return ted25519.Verify(pub, data, sig)
}
