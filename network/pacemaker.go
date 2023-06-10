package network

import (
	"log"
	"sort"

	"github.com/libp2p/go-libp2p/core/peer"
)

type Pacemaker struct {
	b_leaf     *NodeT
	qcHigh     QC
	currLeader uint
}

func (p *Pacemaker) UpdateQcHigh(qcHighprime QC) {
	if qcHighprime.Node.Height > p.qcHigh.Node.Height {
		p.qcHigh = qcHighprime
		p.b_leaf = p.qcHigh.Node
		log.Print("qc high updated")
	}
}

func (p *Pacemaker) OnBeat(cmd []byte, r Replica) {
	if r.network.ID() == p.GetLeader(r.network.Network().Peers(), r.network.ID(), p.currLeader) {
		p.b_leaf = r.OnPropose(cmd)
	}
}

func (p *Pacemaker) GetLeader(peerStore peer.IDSlice, nodeID peer.ID, index uint) peer.ID {
	peerList := append(peerStore, nodeID)

	// sort list of potential leaders to get consistent view in each replica
	sort.Slice(peerList, peerList.Less)
	//p.currLeader++
	return peerList[int(p.currLeader)%peerList.Len()]

}
