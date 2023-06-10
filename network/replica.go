package network

import (
	"bytes"
	"fmt"
	"log"

	"github.com/coinbase/kryptology/pkg/ted25519/ted25519"
	"github.com/karimseh/sharehr/blockchain"
)

type Replica struct {
	ks *KeyShare

	v       map[Hash][]*Msg
	b_lock  *NodeT
	b_exec  *NodeT
	vHeight uint32

	p       Pacemaker
	network *Node
}

func newReplica(ts *KeyShare, n *Node) *Replica {

	r := &Replica{
		ks:      ts,
		network: n,

		v: map[Hash][]*Msg{},
	}
	b0 := GenesisNode(r)
	r.p = Pacemaker{b_leaf: b0, qcHigh: QC{Type: Generic, Node: b0}, currLeader: 0}
	r.b_lock = b0
	r.b_exec = b0
	r.vHeight = r.p.b_leaf.Height

	return r
}
func (r *Replica) Tsign(msg *Msg) *ted25519.PartialSignature {
	hash := msg.Hash(MsgHasher{})
	return ted25519.TSign(hash.ToSlice(), r.ks.Key, r.ks.Pub, r.ks.Nonce, r.ks.NoncePub)
}

func (r *Replica) Verify(qc *QC) bool {
	msgPS := &Msg{
		Type:    qc.Type,
		Node:    *qc.Node,
		Justify: qc.Node.Justify,
	}
	hash := msgPS.Hash(MsgHasher{})

	ok, err := ted25519.Verify(r.ks.Pub, hash.ToSlice(), qc.Sig)
	if err != nil {
		fmt.Print("error verifying sig")
	}
	return ok
}

func (r *Replica) VoteMsg(typ MsgType, node *NodeT, qc QC) Msg {
	msg := NewMsg(typ, *node, qc)
	msg.PartialSig = *r.Tsign(&msg)

	return msg
}

func (r *Replica) CreateLeaf(parent Hash, cmd []byte, qc QC, height uint32) *NodeT {
	node := &NodeT{
		Parent:  parent,
		Cmd:     cmd,
		Justify: qc,
		Height:  height,
	}
	return node
}
func GenesisNode(r *Replica) *NodeT {

	qc := QC{
		Type: Generic,
		Node: nil}

	return r.CreateLeaf(Hash{}, []byte{0}, qc, 1)
}
func (r *Replica) Update(bstar *NodeT) {
	hashOfGenesis := GenesisNode(r).Hash(NodeTHasher{})
	if bstar.Justify.Node.Hash(NodeTHasher{}) == hashOfGenesis {
		//first proposal
		r.p.UpdateQcHigh(bstar.Justify)
		return
	}
	log.Print("updating tree")
	b2prime := bstar.Justify.Node
	bprime := b2prime.Justify.Node
	b := bprime.Justify.Node
	//precommit phase on b2prime
	r.p.UpdateQcHigh(bstar.Justify)
	if bprime.Height > r.b_lock.Height {
		r.b_lock = bprime

	}
	if b != nil && len(b.Cmd) > 1 && b2prime.Parent == bprime.Hash(NodeTHasher{}) && bprime.Parent == b.Hash(NodeTHasher{}) {
		log.Print("going to commit")
		r.OnCommit(b)
		r.b_exec = b
	}
}

func (r *Replica) OnCommit(b *NodeT) {
	if r.b_exec.Height < b.Height {
		//exec b.cmd
		//verify block
		//add block to blockchain
		//delete txs from mempool
		//next view
		log.Print("commiting")
		r.ExecuteCMD(b.Cmd)
	}
}

func (r *Replica) OnPropose(cmd []byte) *NodeT {
	log.Print("On Propose")
	bnew := r.CreateLeaf(r.p.b_leaf.Hash(NodeTHasher{}), cmd, r.p.qcHigh, r.p.b_leaf.Height+1)
	//brodacast proposal
	msgGeneric := NewMsg(Generic, *bnew, QC{})
	encodedMsg, err := SerializeMsg(&msgGeneric)

	if err != nil {
		fmt.Print("Error encoding msg")
	}

	msg := Message{
		Type:    BlockMessage,
		Payload: encodedMsg,
	}
	r.network.Broadcast(&msg)
	log.Print("On Propose sent")
	return bnew
}

func (r *Replica) OnReceiveProppsal(msgGeneric *Msg) {
	log.Print("On receive Proposal ")

	if msgGeneric.Node.Height > r.vHeight &&
		(msgGeneric.Node.Extends(r.b_lock) || msgGeneric.Node.Justify.Node.Height > r.b_lock.Height) {

		r.vHeight = msgGeneric.Node.Height
		votedMsg := r.VoteMsg(msgGeneric.Type, &msgGeneric.Node, msgGeneric.Justify)
		encodedMsg, err := SerializeMsg(&votedMsg)
		if err != nil {
			fmt.Print("Error encoding msg")
		}
		msg := Message{
			Type:    BlockMessage,
			Payload: encodedMsg,
		}
		r.network.SendMessageToPeer(r.p.GetLeader(r.network.Peerstore().Peers(), r.network.ID(), r.p.currLeader), &msg)
		log.Print("Vote sent")
		r.Update(&msgGeneric.Node)

	}
}

func (r *Replica) OnReceiveVote(vote *Msg) {
	//check if vote already counted
	log.Print("Received Vote msg")
	votes, ok := r.v[vote.Node.Hash(NodeTHasher{})]
	if ok {
		if contains(votes, vote) {
			return
		}
		r.v[vote.Node.Hash(NodeTHasher{})] = append(r.v[vote.Node.Hash(NodeTHasher{})], vote) // collect vote

		if len(r.v[vote.Node.Hash(NodeTHasher{})]) >= 2 {
			qc := NewQC(r.v[vote.Node.Hash(NodeTHasher{})])
			log.Print("new qc created ")
			//verify qc.sig before updating highQC
			ok := r.Verify(&qc)
			if ok {
				log.Print("qc.sig verified updating qc")
				r.p.UpdateQcHigh(qc)
			}
		}
	} else {
		r.v[vote.Node.Hash(NodeTHasher{})] = append(r.v[vote.Node.Hash(NodeTHasher{})], vote) // collect 1st vote

	}
}

func (r *Replica) OnNextSyncView() {
	msgNewView := NewMsg(NewView, NodeT{}, r.p.qcHigh)
	encodedMsg, err := SerializeMsg(&msgNewView)
	if err != nil {
		fmt.Print("Error encoding msg")
	}
	msg := Message{
		Type:    BlockMessage,
		Payload: encodedMsg,
	}
	r.p.currLeader++
	r.network.SendMessageToPeer(r.p.GetLeader(r.network.Network().Peers(), r.network.ID(), r.p.currLeader), &msg)

}

func (r *Replica) OnReceiveNewView(msgNewView *Msg) {
	r.p.UpdateQcHigh(msgNewView.Justify)
}

func (r *Replica) ExecuteCMD(cmd []byte) {
	block := new(blockchain.Block)
	block.Decode(blockchain.NewGobBlockDecoder(bytes.NewReader(cmd)))
	err := block.Verify()
	if err != nil {
		log.Print("block not valid, change view:  ", err)
		//change view
		return
	}
	r.network.Blockchain.Addblock(block)
	for _, tx := range block.Transactions {
		r.network.Mempool.DeleteTx(tx)
	}
	//next view
	r.OnNextSyncView()
}
