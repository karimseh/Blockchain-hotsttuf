// consensus.go defines the core data structures for the HotStuff-2 protocol.
//
// HotStuff-2 (Malkhi & Nayak) achieves optimal two-phase responsive BFT:
//   - Two-phase commit: Propose+Vote → Prepare+Vote2 → Commit via double certificate
//   - O(n) optimistic communication, O(n²) worst-case
//   - Optimistic responsiveness with honest leaders
//
// The commit rule is based on a "double certificate" C_v(C_v(B_k)):
//   Phase 1: Leader proposes block, collects 2t+1 votes → forms QC C_v(B_k)
//   Phase 2: Leader broadcasts QC, parties send vote2 → next leader forms double cert
//   When any party sees a double cert, it commits the block.
package network

import "github.com/coinbase/kryptology/pkg/ted25519/ted25519"

// View represents a consensus view number.
type View uint64

// MsgType identifies the type of consensus message.
type MsgType int

const (
	ProposeMsgType MsgType = iota // Leader → All: propose a new block
	VoteMsgType                   // Party → Leader: vote for proposed block
	PrepareMsgType                // Leader → All: broadcast QC formed from votes
	Vote2MsgType                  // Party → Next Leader: confirm seeing QC
	NewViewMsgType                // Party → Leader: send locked cert on view entry
)

// CBlock represents a block in the consensus protocol's block tree.
// It wraps the actual blockchain block data (Cmd) with consensus metadata.
// Corresponds to B_k := (b_k, h_{k-1}) in the paper.
type CBlock struct {
	ParentHash Hash   `json:"parent_hash"` // H(B_{k-1})
	Cmd        []byte `json:"cmd"`         // Encoded blockchain.Block
	Height     uint32 `json:"height"`      // Position in the chain
	View       View   `json:"view"`        // View in which this block was proposed
}

// QC represents a Quorum Certificate C_v(B_k).
// Formed when 2t+1 parties vote for block B_k in view v (Figure 1, Step 4).
// Certificates are ranked by their view number.
type QC struct {
	Block *CBlock            `json:"block"`
	View  View               `json:"view"` // View in which this certificate was formed
	Sig   ted25519.Signature `json:"sig"`
}

// IsHigherOrEqual returns true if this QC is ranked >= other.
// Per the paper: C_v(B_k) is ranked higher than C_v'(B'_k') if v > v'.
func (qc *QC) IsHigherOrEqual(other *QC) bool {
	if other == nil {
		return true
	}
	if qc == nil {
		return false
	}
	return qc.View >= other.View
}

// DoubleCert represents C_v(C_v(B_k)) — a certificate on a certificate.
// Formed when 2t+1 parties send vote2 confirming they've seen QC C_v(B_k).
// This is the HotStuff-2 commit trigger: seeing a DoubleCert means the block is committed.
// Corresponds to the "two-phase commit" that makes HotStuff-2 two-phase instead of three.
type DoubleCert struct {
	CertifiedQC QC                 `json:"certified_qc"` // The inner QC being double-certified
	Sig         ted25519.Signature `json:"sig"`           // Aggregated from vote2 partial signatures
}

// Msg is the unified consensus message envelope.
// Different fields are populated depending on Type.
type Msg struct {
	Type MsgType `json:"type"`
	View View    `json:"view"`

	// Propose fields (Step 2)
	Block  *CBlock     `json:"block,omitempty"`   // Proposed block B_k
	HighQC *QC         `json:"high_qc,omitempty"` // C_{v'}(B_{k-1}): highest cert known to leader
	HighDC *DoubleCert `json:"high_dc,omitempty"` // Highest double cert (triggers commits at receivers)

	// Vote / Vote2: hash of what's being signed
	BlockHash Hash `json:"block_hash"`

	// Prepare: the QC formed from votes; Vote2: the QC being confirmed
	CertQC *QC `json:"cert_qc,omitempty"`

	// NewView: party's locked QC sent to new leader
	LockedQC *QC `json:"locked_qc,omitempty"`

	// Partial threshold signature from sender (Vote and Vote2 messages)
	PartialSig *ted25519.PartialSignature `json:"partial_sig,omitempty"`
}
