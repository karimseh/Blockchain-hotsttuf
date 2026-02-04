// replica.go implements the HotStuff-2 view protocol (Figure 1 from the paper).
//
// Each view v with leader L_v follows these steps:
//   (1) Enter  — Leader/parties enter the view (responsive or with Δ wait)
//   (2) Propose — Leader broadcasts ⟨propose, B_k, v, C_{v'}(B_{k-1}), C_{v''}(C_{v''}(B_{k''}))⟩
//   (3) Vote   — Parties vote if proposal's QC ≥ locked cert; commit from double cert
//   (4) Prepare — Leader aggregates 2t+1 votes into QC C_v(B_k), broadcasts it
//   (5) Vote2  — Parties confirm QC, send vote2 to L_{v+1}; next leader forms double cert
package network

import (
	"bytes"
	"log"
	"sync"
	"time"

	"github.com/coinbase/kryptology/pkg/ted25519/ted25519"
	"github.com/karimseh/sharehr/blockchain"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Replica represents a consensus participant running the HotStuff-2 protocol.
type Replica struct {
	mu sync.Mutex

	ks *KeyShare // Threshold signature key share

	// Current view number
	currentView View

	// Locked block and its certificate — guards safety of commits.
	// Per the paper: "a party locks the highest certified block to its knowledge"
	lockedQC *QC

	// Last executed (committed) block height
	execHeight uint32

	// Highest double certificate seen — triggers commits
	highDC *DoubleCert

	// Pacemaker state: highest QC and current leaf block
	highQC    *QC
	leafBlock *CBlock

	// Vote collection (leader only)
	votes  map[Hash][]*Msg // blockHash → vote messages for Step 4
	vote2s map[Hash][]*Msg // qcHash → vote2 messages for double cert

	// Track whether we voted in the current view (each party votes at most once)
	votedInView View

	// Configuration: n = 3t + 1
	n      int // Total parties
	t      int // Max Byzantine faults
	quorum int // 2t + 1

	// View timer for timeout (pacemaker)
	viewTimer *time.Timer
	delta     time.Duration // Network delay bound Δ

	// pendingCmds stores block commands by CBlock height so that
	// commitFromDoubleCert can execute all ancestors in order
	// (per Figure 1, Step 3: "commits block B_{k''} and all its ancestors").
	pendingCmds map[uint32][]byte

	// Minimum interval between proposals to allow tx collection and
	// orderly commit processing. Zero means no delay (pure responsive).
	minBlockInterval time.Duration
	lastProposeTime  time.Time

	network Transport
}

func newReplica(ks *KeyShare, n Transport, numParties, faultyThreshold int) *Replica {
	r := &Replica{
		ks:               ks,
		currentView:      0,
		n:                numParties,
		t:                faultyThreshold,
		quorum:           2*faultyThreshold + 1,
		votes:            make(map[Hash][]*Msg),
		vote2s:           make(map[Hash][]*Msg),
		pendingCmds:      make(map[uint32][]byte),
		delta:            2 * time.Second, // Δ for the prototype
		minBlockInterval: 2000 * time.Millisecond,
		network:          n,
	}

	// Genesis block and QC
	genesis := &CBlock{
		ParentHash: Hash{},
		Cmd:        nil,
		Height:     0,
		View:       0,
	}
	genesisQC := &QC{
		Block: genesis,
		View:  0,
	}

	r.lockedQC = genesisQC
	r.highQC = genesisQC
	r.leafBlock = genesis

	return r
}

// --- Threshold Signature Operations ---

func (r *Replica) sign(data []byte) *ted25519.PartialSignature {
	// NOTE: In production, fresh nonces must be generated per signing operation.
	// The current prototype reuses pre-generated nonces for simplicity.
	return ted25519.TSign(data, r.ks.Key, r.ks.Pub, r.ks.Nonce, r.ks.NoncePub)
}

func (r *Replica) aggregateSigs(partialSigs []*ted25519.PartialSignature) (ted25519.Signature, error) {
	return ted25519.Aggregate(partialSigs, &ted25519.ShareConfiguration{
		T: r.quorum,
		N: r.n,
	})
}

// --- Figure 1, Step 1: Enter View ---

// EnterView transitions the replica to a new view.
// doubleCert is non-nil if entering via Case 1 (responsive, from double cert).
// If nil, this is Case 2 (timeout-triggered, leader waits P_pc + Δ).
func (r *Replica) EnterView(v View, doubleCert *DoubleCert) {
	r.mu.Lock()

	if v <= r.currentView && r.currentView > 0 {
		r.mu.Unlock()
		return
	}

	log.Printf("[View %d → %d] Entering view", r.currentView, v)
	r.currentView = v
	r.votes = make(map[Hash][]*Msg)
	r.vote2s = make(map[Hash][]*Msg)

	// Stop any existing view timer
	if r.viewTimer != nil {
		r.viewTimer.Stop()
	}

	isLeader := r.isLeader(v)

	if doubleCert != nil {
		// Case 1: Entering via double certificate — responsive path
		// Per Figure 1, Step 1: Leader proceeds directly to propose
		if isLeader {
			r.mu.Unlock()
			r.propose()
			return
		}
		// Non-leader: proceed to vote step (wait for proposal)
	} else {
		// Case 2: Entering without double certificate
		// Leader waits P_pc + Δ = 3Δ for parties to send their locked certs
		if isLeader {
			waitTime := 3 * r.delta
			log.Printf("[View %d] Leader waiting %v for locked certs", v, waitTime)
			r.mu.Unlock()
			time.Sleep(waitTime)
			r.propose()
			return
		}
		// Non-leader: send locked certificate to the leader
		r.sendLockedCertToLeader()
	}

	// Start view timeout timer
	r.viewTimer = time.AfterFunc(6*r.delta, func() {
		r.onViewTimeout()
	})

	r.mu.Unlock()
}

// --- Figure 1, Step 2: Propose (Leader only) ---

func (r *Replica) propose() {
	r.mu.Lock()

	v := r.currentView
	if !r.isLeader(v) {
		r.mu.Unlock()
		return
	}

	// Enforce minimum block interval to allow tx collection and orderly commits.
	if r.minBlockInterval > 0 && !r.lastProposeTime.IsZero() {
		elapsed := time.Since(r.lastProposeTime)
		if elapsed < r.minBlockInterval {
			wait := r.minBlockInterval - elapsed
			r.mu.Unlock()
			time.Sleep(wait)
			r.mu.Lock()
		}
	}
	r.lastProposeTime = time.Now()

	log.Printf("[View %d] Leader proposing block at height %d", v, r.leafBlock.Height+1)

	// Build the actual blockchain block as the command payload
	cmd := r.buildBlockCmd()

	// Create new consensus block extending the highest certified block
	parentBlock := r.highQC.Block
	block := &CBlock{
		ParentHash: parentBlock.Hash(CBlockHasher{}),
		Cmd:        cmd,
		Height:     parentBlock.Height + 1,
		View:       v,
	}

	// Broadcast proposal: ⟨propose, B_k, v, C_{v'}(B_{k-1}), C_{v''}(C_{v''}(B_{k''}))⟩
	msg := &Msg{
		Type:   ProposeMsgType,
		View:   v,
		Block:  block,
		HighQC: r.highQC,
		HighDC: r.highDC,
	}

	// Store cmd for ancestor commits
	r.pendingCmds[block.Height] = cmd

	r.leafBlock = block
	r.broadcastConsensusMsg(msg)
	log.Printf("[View %d] Proposal broadcast for height %d", v, block.Height)
	r.mu.Unlock()
}

// --- Figure 1, Step 3: Vote and Commit ---

// OnReceiveProposal handles an incoming proposal message.
func (r *Replica) OnReceiveProposal(msg *Msg) {
	r.mu.Lock()

	// Per Pacemaker Figure 2, Step 3: a party enters view v' upon receiving
	// a double cert C_{v'-1}(C_{v'-1}(B_{k'})). If the proposal is for a
	// future view and carries a double cert, advance responsively first.
	if msg.View > r.currentView && msg.HighDC != nil {
		r.mu.Unlock()
		r.EnterView(msg.View, msg.HighDC)
		r.mu.Lock()
	}

	if msg.View != r.currentView {
		r.mu.Unlock()
		return
	}
	if r.votedInView == r.currentView {
		r.mu.Unlock()
		return
	}

	log.Printf("[View %d] Received proposal for height %d", r.currentView, msg.Block.Height)

	// Store block command for ancestor commit ordering
	if msg.Block.Cmd != nil {
		r.pendingCmds[msg.Block.Height] = msg.Block.Cmd
	}

	// Commit if proposal contains a double certificate
	// Per Step 3: "The party commits block B_{k''} and all its ancestors."
	if msg.HighDC != nil {
		r.commitFromDoubleCert(msg.HighDC)
	}

	// Safety check: proposal's QC must be ranked no lower than our locked cert
	// Per Step 3: "If C_{v'}(B_{k-1}) is ranked no lower than the locked block"
	if msg.HighQC == nil || !msg.HighQC.IsHigherOrEqual(r.lockedQC) {
		log.Printf("[View %d] Proposal QC (view %d) ranked lower than locked cert (view %d), not voting",
			r.currentView, msg.HighQC.View, r.lockedQC.View)
		r.mu.Unlock()
		return
	}

	// Update lock to B_{k-1} and the certificate to C_{v'}(B_{k-1})
	if msg.HighQC.View > r.lockedQC.View {
		r.lockedQC = msg.HighQC
	}

	// Update highQC if proposal's QC is higher
	if msg.HighQC.View > r.highQC.View {
		r.highQC = msg.HighQC
		r.leafBlock = msg.HighQC.Block
	}

	// Vote for the proposed block
	r.votedInView = r.currentView
	blockHash := msg.Block.Hash(CBlockHasher{})

	voteMsg := &Msg{
		Type:       VoteMsgType,
		View:       r.currentView,
		BlockHash:  blockHash,
		Block:      msg.Block,
		PartialSig: r.sign(blockHash.ToSlice()),
	}

	leaderID := r.getLeader(r.currentView)
	r.sendConsensusMsg(leaderID, voteMsg)
	log.Printf("[View %d] Vote sent for height %d", r.currentView, msg.Block.Height)
	r.mu.Unlock()
}

// --- Figure 1, Step 4: Prepare (Leader aggregates votes into QC) ---

// OnReceiveVote handles an incoming vote message (leader only).
func (r *Replica) OnReceiveVote(msg *Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg.View != r.currentView || !r.isLeader(r.currentView) {
		return
	}

	blockHash := msg.BlockHash
	votes := r.votes[blockHash]

	// Deduplicate by share identifier
	if msg.PartialSig != nil {
		for _, v := range votes {
			if v.PartialSig != nil && v.PartialSig.ShareIdentifier == msg.PartialSig.ShareIdentifier {
				return
			}
		}
	}

	r.votes[blockHash] = append(r.votes[blockHash], msg)
	log.Printf("[View %d] Received vote %d/%d", r.currentView, len(r.votes[blockHash]), r.quorum)

	if len(r.votes[blockHash]) >= r.quorum {
		log.Printf("[View %d] Quorum reached, forming QC", r.currentView)

		// Aggregate partial signatures into QC
		partialSigs := make([]*ted25519.PartialSignature, 0, r.quorum)
		for _, v := range r.votes[blockHash] {
			if v.PartialSig != nil {
				partialSigs = append(partialSigs, v.PartialSig)
			}
		}

		sig, err := r.aggregateSigs(partialSigs)
		if err != nil {
			log.Printf("[View %d] Error aggregating vote signatures: %v", r.currentView, err)
			return
		}

		qc := &QC{
			Block: msg.Block,
			View:  r.currentView,
			Sig:   sig,
		}

		// Update our highQC
		if qc.View > r.highQC.View {
			r.highQC = qc
			r.leafBlock = qc.Block
		}

		// Broadcast prepare with the QC: ⟨prepare, C_v(B_k)⟩
		prepareMsg := &Msg{
			Type:   PrepareMsgType,
			View:   r.currentView,
			CertQC: qc,
		}
		r.broadcastConsensusMsg(prepareMsg)
		log.Printf("[View %d] Prepare broadcast with QC for height %d", r.currentView, qc.Block.Height)
	}
}

// --- Figure 1, Step 5: Vote2 (on receiving prepare) ---

// OnReceivePrepare handles the leader's prepare message containing the QC.
func (r *Replica) OnReceivePrepare(msg *Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg.View != r.currentView || msg.CertQC == nil {
		return
	}

	log.Printf("[View %d] Received prepare, updating lock to height %d",
		r.currentView, msg.CertQC.Block.Height)

	// Update lock to B_k and certificate to C_v(B_k)
	r.lockedQC = msg.CertQC
	if msg.CertQC.View > r.highQC.View {
		r.highQC = msg.CertQC
		r.leafBlock = msg.CertQC.Block
	}

	// Send vote2 to L_{v+1}: ⟨vote2, C_v(B_k), v⟩
	qcHash := QCHasher{}.Hash(msg.CertQC)

	vote2Msg := &Msg{
		Type:       Vote2MsgType,
		View:       r.currentView,
		CertQC:     msg.CertQC,
		BlockHash:  qcHash,
		PartialSig: r.sign(qcHash.ToSlice()),
	}

	nextLeaderID := r.getLeader(r.currentView + 1)
	r.sendConsensusMsg(nextLeaderID, vote2Msg)
	log.Printf("[View %d] Vote2 sent to leader of view %d", r.currentView, r.currentView+1)
}

// OnReceiveVote2 handles vote2 messages. The next leader collects these
// to form the double certificate C_v(C_v(B_k)).
func (r *Replica) OnReceiveVote2(msg *Msg) {
	r.mu.Lock()

	nextView := msg.View + 1
	if !r.isLeader(nextView) {
		r.mu.Unlock()
		return
	}

	qcHash := msg.BlockHash
	vote2s := r.vote2s[qcHash]

	// Deduplicate
	if msg.PartialSig != nil {
		for _, v := range vote2s {
			if v.PartialSig != nil && v.PartialSig.ShareIdentifier == msg.PartialSig.ShareIdentifier {
				r.mu.Unlock()
				return
			}
		}
	}

	r.vote2s[qcHash] = append(r.vote2s[qcHash], msg)
	log.Printf("[View %d] Received vote2 %d/%d (for next view %d)",
		r.currentView, len(r.vote2s[qcHash]), r.quorum, nextView)

	if len(r.vote2s[qcHash]) < r.quorum {
		r.mu.Unlock()
		return
	}

	log.Printf("Double cert quorum reached! Forming C_v(C_v(B_k))")

	// Aggregate vote2 partial signatures into double cert
	partialSigs := make([]*ted25519.PartialSignature, 0, r.quorum)
	for _, v := range r.vote2s[qcHash] {
		if v.PartialSig != nil {
			partialSigs = append(partialSigs, v.PartialSig)
		}
	}

	sig, err := r.aggregateSigs(partialSigs)
	if err != nil {
		log.Printf("Error aggregating vote2 signatures: %v", err)
		r.mu.Unlock()
		return
	}

	dc := &DoubleCert{
		CertifiedQC: *msg.CertQC,
		Sig:          sig,
	}

	// Update highest double cert
	if r.highDC == nil || msg.CertQC.View > r.highDC.CertifiedQC.View {
		r.highDC = dc
	}

	// Update highQC from the certified QC
	if msg.CertQC.View > r.highQC.View {
		r.highQC = msg.CertQC
		r.leafBlock = msg.CertQC.Block
	}

	// Advance to next view using double cert — Case 1 (responsive)
	// Per Pacemaker Figure 2, Step 3: advance upon receiving C_{v'-1}(C_{v'-1}(B_{k'}))
	log.Printf("★ Double cert formed for view %d, advancing to view %d", msg.View, nextView)
	r.mu.Unlock()
	r.EnterView(nextView, dc)
}

// OnReceiveNewView handles a party sending its locked certificate to the new leader.
// Per Figure 1, Step 1 (Case 2): parties send locked cert to leader.
func (r *Replica) OnReceiveNewView(msg *Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg.LockedQC != nil && msg.LockedQC.View > r.highQC.View {
		r.highQC = msg.LockedQC
		r.leafBlock = msg.LockedQC.Block
		log.Printf("[View %d] Updated highQC from NewView to view %d", r.currentView, msg.LockedQC.View)
	}
}

// --- Commit Logic ---

// commitFromDoubleCert commits a certified block AND all its uncommitted ancestors.
// Per Figure 1, Step 3: "The party commits block B_{k''} and all its ancestors."
func (r *Replica) commitFromDoubleCert(dc *DoubleCert) {
	block := dc.CertifiedQC.Block
	if block == nil || block.Height <= r.execHeight {
		return
	}

	targetHeight := block.Height

	// Execute all pending blocks from execHeight+1 up to targetHeight in order.
	for h := r.execHeight + 1; h <= targetHeight; h++ {
		cmd, ok := r.pendingCmds[h]
		if !ok {
			log.Printf("[View %d] Missing pending cmd for height %d, cannot commit ancestors", r.currentView, h)
			break
		}
		log.Printf("[View %d] ★ COMMITTING block at height %d", r.currentView, h)
		r.executeCmd(cmd)
		r.execHeight = h
		delete(r.pendingCmds, h)
	}

	if r.highDC == nil || dc.CertifiedQC.View > r.highDC.CertifiedQC.View {
		r.highDC = dc
	}
}

func (r *Replica) executeCmd(cmd []byte) {
	if len(cmd) == 0 {
		return
	}

	block := new(blockchain.Block)
	if err := block.Decode(blockchain.NewGobBlockDecoder(bytes.NewReader(cmd))); err != nil {
		log.Printf("Error decoding block: %v", err)
		return
	}
	if err := block.Verify(); err != nil {
		log.Printf("Block verification failed: %v", err)
		return
	}

	if err := r.network.GetBlockchain().Addblock(block); err != nil {
		log.Printf("Block rejected by chain validation: %v", err)
		return
	}
	for _, tx := range block.Transactions {
		r.network.GetMempool().DeleteTx(tx)
	}
	log.Printf("Block committed to blockchain at height %d", r.network.GetBlockchain().Height())
}

// --- View Timeout (Pacemaker, simplified) ---

func (r *Replica) onViewTimeout() {
	r.mu.Lock()
	v := r.currentView
	log.Printf("[View %d] View timeout! Advancing to next view", v)
	r.mu.Unlock()

	// Send NewView with locked QC to next leader, then advance
	r.mu.Lock()
	msg := &Msg{
		Type:     NewViewMsgType,
		View:     v + 1,
		LockedQC: r.lockedQC,
	}
	nextLeader := r.getLeader(v + 1)
	r.mu.Unlock()

	r.sendConsensusMsgUnlocked(nextLeader, msg)
	r.EnterView(v+1, nil)
}

// --- Helper Methods ---

func (r *Replica) buildBlockCmd() []byte {
	// Determine the previous block header for chaining.
	// With pipelining, the parent CBlock (from highQC) may contain a blockchain
	// block that hasn't been committed yet. Extract its header so the new block
	// chains correctly even when the committed chain lags behind consensus.
	prevHeader := r.network.GetBlockchain().GetLastHeader()
	if r.highQC != nil && r.highQC.Block != nil && len(r.highQC.Block.Cmd) > 0 {
		parentBlock := new(blockchain.Block)
		if err := parentBlock.Decode(blockchain.NewGobBlockDecoder(bytes.NewReader(r.highQC.Block.Cmd))); err == nil {
			if parentBlock.Header.BestHeight >= prevHeader.BestHeight {
				prevHeader = parentBlock.Header
			}
		}
	}

	txs := r.network.GetMempool().GetTxs()
	newBlock, err := blockchain.NewBlockFromPreviousHeader(prevHeader, txs)
	if err != nil {
		log.Printf("Error creating block: %v", err)
		return nil
	}
	if err := newBlock.Sign(r.network.GetWallet()); err != nil {
		log.Printf("Error signing block: %v", err)
		return nil
	}

	buf := &bytes.Buffer{}
	if err := newBlock.Encode(blockchain.NewGobBlockEncoder(buf)); err != nil {
		log.Printf("Error encoding block: %v", err)
		return nil
	}
	return buf.Bytes()
}

func (r *Replica) sendLockedCertToLeader() {
	msg := &Msg{
		Type:     NewViewMsgType,
		View:     r.currentView,
		LockedQC: r.lockedQC,
	}
	leaderID := r.getLeader(r.currentView)
	go func() {
		r.sendConsensusMsgUnlocked(leaderID, msg)
	}()
}

// broadcastConsensusMsg sends a consensus message to all peers via pubsub.
// Caller must hold r.mu.
func (r *Replica) broadcastConsensusMsg(msg *Msg) {
	encoded, err := SerializeMsg(msg)
	if err != nil {
		log.Printf("Error encoding consensus msg: %v", err)
		return
	}
	netMsg := &Message{
		Type:    BlockMessage,
		Payload: encoded,
	}
	r.network.Broadcast(netMsg)
}

// sendConsensusMsg sends a consensus message to a specific peer.
// Caller must hold r.mu.
func (r *Replica) sendConsensusMsg(peerID peer.ID, msg *Msg) {
	go func() {
		r.sendConsensusMsgUnlocked(peerID, msg)
	}()
}

// sendConsensusMsgUnlocked sends without requiring the lock.
// If the target is self, the message is routed locally to avoid
// libp2p's "dial to self" error.
func (r *Replica) sendConsensusMsgUnlocked(peerID peer.ID, msg *Msg) {
	if peerID == r.network.ID() {
		r.routeLocalMsg(msg)
		return
	}
	encoded, err := SerializeMsg(msg)
	if err != nil {
		log.Printf("Error encoding consensus msg: %v", err)
		return
	}
	netMsg := &Message{
		Type:    BlockMessage,
		Payload: encoded,
	}
	if err := r.network.SendMessageToPeer(peerID, netMsg); err != nil {
		log.Printf("Error sending to peer %s: %v", peerID.String(), err)
	}
}

// routeLocalMsg dispatches a consensus message to the local handler,
// used when the target peer is this node itself.
func (r *Replica) routeLocalMsg(msg *Msg) {
	switch msg.Type {
	case VoteMsgType:
		r.OnReceiveVote(msg)
	case Vote2MsgType:
		r.OnReceiveVote2(msg)
	case NewViewMsgType:
		r.OnReceiveNewView(msg)
	}
}
