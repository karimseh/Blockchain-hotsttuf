package network

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/karimseh/sharehr/blockchain"
	"github.com/karimseh/sharehr/wallet"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type Context struct {
	Ctx    context.Context
	Cancel func()
}

type Node struct {
	host.Host
	C            Context
	Dht          *dht.IpfsDHT
	Bootstrap    bool
	Topic        *pubsub.Topic
	Subscription *pubsub.Subscription
	Wallet       wallet.Wallet

	lastIndexSent int
	keyshares     map[int]KeyShare

	Blockchain *blockchain.Blockchain
	Mempool    *blockchain.Mempool
}

var r *Replica

func NewContext(ctx context.Context, cancel func()) Context {
	return Context{
		Ctx:    ctx,
		Cancel: cancel,
	}
}

func NewNode(C Context, port int, bootstrap bool) (*Node, error) {

	wallet := wallet.NewWallet()
	addr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
	privKey, _, _ := crypto.ECDSAKeyPairFromKey(wallet.PrivKey)
	host, err := libp2p.New(
		libp2p.ListenAddrs(addr),
		libp2p.Identity(privKey),
	)
	if err != nil {
		return nil, err
	}
	if bootstrap {
		return &Node{
			Host:          host,
			C:             C,
			Wallet:        wallet,
			Bootstrap:     bootstrap,
			lastIndexSent: 1,
			keyshares:     NewKeyShareMap(),
		}, nil
	}

	return &Node{
		Host:      host,
		C:         C,
		Wallet:    wallet,
		Bootstrap: bootstrap,
	}, nil

}
func (node *Node) Discover(rendezvous string) {
	routingDiscovery := discovery.NewRoutingDiscovery(node.Dht)
	routingDiscovery.Advertise(node.C.Ctx, rendezvous)
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()

	for {
		select {
		case <-node.C.Ctx.Done():
			return
		case <-ticker.C:
			log.Printf("Rotuing Discovery ...")
			log.Print("My Blockchain :", node.Blockchain.String())
			// dest := types.AddressFromBytes([]byte("TestTestTestTestTest"))
			// data := []byte("transaction-data")
			// tx := blockchain.NewTransaction(dest, data)
			// buf := &bytes.Buffer{}
			// tx.Encode(blockchain.NewGobTxEncoder(buf))

			// if node.Bootstrap {
			// 	node.Broadcast(&Message{
			// 		Type:    TxMessage,
			// 		Payload: buf.Bytes(),
			// 	})
			// }
			peers, err := routingDiscovery.FindPeers(node.C.Ctx, rendezvous)
			if err != nil {
				log.Fatal(err)
			}
			for p := range peers {
				if p.ID == node.ID() {
					continue
				}
				if node.Network().Connectedness(p.ID) != network.Connected {
					err := node.Connect(node.C.Ctx, p)
					if err != nil {
						log.Printf("Cannot connect to peer %s\n", p.ID.Pretty())

						continue
					}
					log.Printf("Connected to peer %s\n", p.ID.Pretty())

				}
			}
		}
	}
}

func (node *Node) ConnectToBootsrap(bootstrapPeers []multiaddr.Multiaddr) {
	var wg sync.WaitGroup
	for _, peerAddr := range bootstrapPeers {
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := node.Connect(node.C.Ctx, *peerinfo); err != nil {
				log.Printf("Error while connecting to node %q: %-v", peerinfo, err)
			} else {
				log.Printf("Connection established with bootstrap node: %q", *peerinfo)
				//send Init MSG to get keys and initialize replica
				inf, _ := node.Host.ID().Marshal()
				msg := Message{
					Type:    InitMessage,
					Payload: inf,
				}
				node.SendMessageToPeer(peerinfo.ID, &msg)
			}
		}()
	}
	wg.Wait()
}

func (node *Node) InitDht() error {
	var options []dht.Option
	if node.Bootstrap {
		options = append(options, dht.Mode(dht.ModeServer))
	}

	kdht, err := dht.New(node.C.Ctx, node.Host, options...)
	if err != nil {
		return err
	}
	node.Dht = kdht
	if err := node.Dht.Bootstrap(node.C.Ctx); err != nil {
		return err
	}

	return nil
}

func (node *Node) Start(rendezvous string, bootstrapPeers []multiaddr.Multiaddr) {
	log.Printf("Host ID: %s", node.ID().Pretty())
	log.Printf("Connect to me on:")
	for _, addr := range node.Addrs() {
		log.Printf("  %s/p2p/%s", addr, node.ID().Pretty())
	}
	if err := node.InitDht(); err != nil {
		log.Printf("DHT Init Error !")
		panic(err)
	}
	if node.Bootstrap {
		ks := node.keyshares[0]
		r = newReplica(&ks, node)
	}
	node.ConnectToBootsrap(bootstrapPeers)
	node.InitBlockchain()
	go node.Discover(rendezvous)

	go node.startListening()

	go node.SendTx()

	go node.ProposeBlock()

	c := make(chan os.Signal, 1)

	signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	<-c

	fmt.Printf("\rExiting...\n")

	node.C.Cancel()

	if err := node.Close(); err != nil {
		panic(err)
	}
	os.Exit(0)

}
func (node *Node) ProposeBlock() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-node.C.Ctx.Done():
			return
		case <-ticker.C:
			if len(node.Peerstore().Peers()) >= 4 && node.ID() == r.p.GetLeader(r.network.Network().Peers(), r.network.ID(), r.p.currLeader) {

				cmd := &bytes.Buffer{}
				newBlock, _ := node.NextBlock()

				err := newBlock.Encode(blockchain.NewGobBlockEncoder(cmd))
				if err != nil {
					fmt.Print("error encoding new block")

				}
				r.p.OnBeat(cmd.Bytes(), *r)
				log.Print("On Beat sent")
			}
		}
	}

}
func (node *Node) SendTx() {
	if node.Bootstrap {

		ticker := time.NewTicker(time.Second * 30)
		defer ticker.Stop()

		for {
			select {
			case <-node.C.Ctx.Done():
				return
			case <-ticker.C:
				tx := NewRandomTx(node.Wallet)
				buf := &bytes.Buffer{}
				_ = tx.Encode(blockchain.NewGobTxEncoder(buf))
				msg := Message{Type: TxMessage, Payload: buf.Bytes()}
				node.Broadcast(&msg)
			}
		}
	}

}
func (node *Node) handleStream(stream network.Stream) {
	data, err := io.ReadAll(stream)
	if err != nil {
		// Handle error
		return
	}
	msg, err := DeserializeMessage(data)
	if err != nil {
		// Handle error
		return
	}

	// Process the received message
	// ...
	switch msg.Type {
	case InitMessage:
		if node.Bootstrap {
			//respond with keys
			log.Print("received INIT message")
			peer, err := peer.IDFromBytes(msg.Payload)
			if err != nil {
				log.Print("error getting peer id")
			}
			m := node.keyshares[node.lastIndexSent]
			msg := Message{
				Type:    InitMessage,
				Payload: ConvertKeyShareToBytes(m),
			}
			node.SendMessageToPeer(peer, &msg)
			if err != nil {
				log.Print("error sending key share")
			}
			node.lastIndexSent++
		} else {
			//initiate replica with keys
			log.Print("received key share")

			ks, err := DecodeBytesToKeyShare(msg.Payload)
			if err != nil {
				log.Print("error decoding key share")
			}

			r = newReplica(&ks, node)
			//fmt.Print(r.p.GetLeader(r.network.Network().Peers(), r.p.currLeader))
		}
	case BlockMessage:
		msgBlock := &Msg{}
		msgBlock, err := DeserializeMsg(msg.Payload)

		if err != nil {
			log.Print("error decoding generic message")
		}
		switch msgBlock.Type {
		case Generic:
			//On receive vote
			r.OnReceiveVote(msgBlock)
		case NewView:
			r.OnReceiveNewView(msgBlock)

		}
	case GetDataMessage:
		//for sync

	}
	// Close the stream
	_ = stream.Close()
}

func (node *Node) startListening() {
	node.SetStreamHandler("sharehr", node.handleStream)
	ps, err := pubsub.NewGossipSub(node.C.Ctx, node.Host)
	if err != nil {
		panic(err)
	}

	topic, err := ps.Join("sharehr")
	if err != nil {
		fmt.Print("error topic join")
	}
	node.Topic = topic

	sub, err := node.Topic.Subscribe()
	if err != nil {
		fmt.Print("error topic subscribe")
	}
	node.Subscription = sub

	node.HandleBroadcast()

}

func (node *Node) SendMessageToPeer(peerID peer.ID, msg *Message) error {
	stream, err := node.NewStream(node.C.Ctx, peerID, "sharehr")
	if err != nil {
		return err
	}

	data, err := SerializeMessage(msg)
	if err != nil {
		return err
	}

	_, err = stream.Write(data)
	if err != nil {
		return err
	}

	err = stream.Close()
	if err != nil {
		return err
	}

	return nil
}

// broadcast
func (node *Node) Broadcast(msg *Message) {
	msgToBytes, _ := SerializeMessage(msg)
	if err := node.Topic.Publish(node.C.Ctx, msgToBytes); err != nil {
		fmt.Println("### Broadcast error:", err)
	}
}

// receive Broadcast
func (node *Node) HandleBroadcast() {
	for {
		m, err := node.Subscription.Next(node.C.Ctx)
		if err != nil {
			panic(err)
		}
		msg, err := DeserializeMessage(m.Message.Data)
		if err != nil {
			fmt.Print("Error deserilazing msg")
		}
		switch msg.Type {
		case BlockMessage:
			msgBlock := &Msg{}
			msgBlock, err := DeserializeMsg(msg.Payload)

			if err != nil {
				fmt.Print("error decoding msg")
			}
			switch msgBlock.Type {
			case Generic:
				//OnReceiveProposal
				r.OnReceiveProppsal(msgBlock)
			}
		case TxMessage:
			log.Print("received TX mssage")
			tx := new(blockchain.Transaction)
			tx.Decode(blockchain.NewGobTxDecoder(bytes.NewReader(msg.Payload)))
			node.Mempool.AddTx(tx)
			//if leader create block.

		}
	}
}

func (node *Node) InitBlockchain() {
	log.Printf("initialization of blockchain")
	h := blockchain.Header{
		TxHash:        blockchain.Hash{},
		PrevBlockHash: blockchain.Hash{},
		BestHeight:    1,
		Timestamp:     0,
	}
	genesis := blockchain.NewBlock(&h, []*blockchain.Transaction{})

	node.Blockchain = blockchain.NewBlockchain(genesis)
	node.Mempool = blockchain.NewMempool()
}

func (node *Node) NextBlock() (*blockchain.Block, error) {

	lastHeader := node.Blockchain.GetLastHeader()
	txs := node.Mempool.GetTxs()
	fmt.Print(txs)
	newBlock, err := blockchain.NewBlockFromPreviousHeader(lastHeader, txs)
	newBlock.Sign(node.Wallet)
	if err != nil {
		log.Print("error creating block!!")
		return nil, err
	}

	return newBlock, nil

}
