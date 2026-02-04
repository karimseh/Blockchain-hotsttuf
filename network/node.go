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

	Replica *Replica
}

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
			log.Printf("Routing Discovery ...")
			log.Print("My Blockchain: ", node.Blockchain.String())

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
						log.Printf("Cannot connect to peer %s\n", p.ID.String())
						continue
					}
					log.Printf("Connected to peer %s\n", p.ID.String())
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
				// Send Init MSG to get keys and initialize replica
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
	// All nodes run as DHT servers so they actively participate in
	// routing and peer discovery propagates reliably.
	kdht, err := dht.New(node.C.Ctx, node.Host, dht.Mode(dht.ModeServer))
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
	log.Printf("Host ID: %s", node.ID().String())
	log.Printf("Connect to me on:")
	for _, addr := range node.Addrs() {
		log.Printf("  %s/p2p/%s", addr, node.ID().String())
	}

	// Init blockchain and mempool first — Replica needs these immediately
	node.InitBlockchain()

	if err := node.InitDht(); err != nil {
		log.Printf("DHT Init Error!")
		panic(err)
	}

	if node.Bootstrap {
		ks := node.keyshares[0]
		node.Replica = newReplica(&ks, node, NumParties, FaultyThreshold)
	}

	// Register direct stream handler before connecting so we can
	// receive InitMessage responses from the bootstrap node
	node.SetStreamHandler("sharehr", node.handleStream)
	go node.startPubSub()

	// Write peer ID to file if requested (for Docker orchestration)
	if peerIDFile := os.Getenv("WRITE_PEER_ID"); peerIDFile != "" {
		if err := os.WriteFile(peerIDFile, []byte(node.ID().String()), 0644); err != nil {
			log.Printf("Error writing peer ID file: %v", err)
		}
	}

	node.ConnectToBootsrap(bootstrapPeers)
	go node.Discover(rendezvous)
	go node.SendTx()

	// Wait for Replica to be initialized (non-bootstrap gets it via InitMessage),
	// then start consensus once enough peers are connected
	go node.waitForReplicaAndStart()

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

// waitForReplicaAndStart polls until the Replica is initialized
// (non-bootstrap nodes receive their key share asynchronously),
// then starts consensus.
func (node *Node) waitForReplicaAndStart() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if node.Replica != nil {
			node.Replica.WaitForPeersAndStart()
			return
		}
		log.Printf("Waiting for Replica initialization...")
	}
}

func (node *Node) SendTx() {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-node.C.Ctx.Done():
			return
		case <-ticker.C:
			if node.Topic == nil {
				continue // pubsub not ready yet
			}
			tx := NewRandomTx(node.Wallet)
			buf := &bytes.Buffer{}
			_ = tx.Encode(blockchain.NewGobTxEncoder(buf))
			msg := Message{Type: TxMessage, Payload: buf.Bytes()}
			node.Broadcast(&msg)
		}
	}
}

// handleStream processes incoming direct (stream) messages.
// Direct messages are used for: InitMessage, Vote, Vote2, NewView.
func (node *Node) handleStream(stream network.Stream) {
	data, err := io.ReadAll(stream)
	if err != nil {
		return
	}
	_ = stream.Close()

	msg, err := DeserializeMessage(data)
	if err != nil {
		return
	}

	switch msg.Type {
	case InitMessage:
		node.handleInitMessage(msg)

	case BlockMessage:
		// Consensus message received via direct stream
		consensusMsg, err := DeserializeMsg(msg.Payload)
		if err != nil {
			log.Printf("Error decoding consensus message: %v", err)
			return
		}
		node.routeConsensusMsg(consensusMsg)
	}
}

// handleInitMessage handles key share distribution during node bootstrap.
func (node *Node) handleInitMessage(msg *Message) {
	if node.Bootstrap {
		// Bootstrap node: respond with key share
		log.Print("Received INIT message")
		peerID, err := peer.IDFromBytes(msg.Payload)
		if err != nil {
			log.Print("Error getting peer id")
			return
		}
		m := node.keyshares[node.lastIndexSent]
		resp := Message{
			Type:    InitMessage,
			Payload: ConvertKeyShareToBytes(m),
		}
		node.SendMessageToPeer(peerID, &resp)
		node.lastIndexSent++
	} else {
		// Non-bootstrap node: initialize replica with received key share
		log.Print("Received key share")
		ks, err := DecodeBytesToKeyShare(msg.Payload)
		if err != nil {
			log.Print("Error decoding key share")
			return
		}
		node.Replica = newReplica(&ks, node, NumParties, FaultyThreshold)
	}
}

// routeConsensusMsg dispatches a consensus message to the appropriate handler.
func (node *Node) routeConsensusMsg(msg *Msg) {
	if node.Replica == nil {
		log.Print("Replica not initialized, ignoring consensus message")
		return
	}

	switch msg.Type {
	case ProposeMsgType:
		node.Replica.OnReceiveProposal(msg)
	case VoteMsgType:
		node.Replica.OnReceiveVote(msg)
	case PrepareMsgType:
		node.Replica.OnReceivePrepare(msg)
	case Vote2MsgType:
		node.Replica.OnReceiveVote2(msg)
	case NewViewMsgType:
		node.Replica.OnReceiveNewView(msg)
	default:
		log.Printf("Unknown consensus message type: %d", msg.Type)
	}
}

func (node *Node) startPubSub() {
	ps, err := pubsub.NewGossipSub(node.C.Ctx, node.Host)
	if err != nil {
		panic(err)
	}

	topic, err := ps.Join("sharehr")
	if err != nil {
		log.Printf("Error joining topic: %v", err)
		return
	}
	node.Topic = topic

	sub, err := node.Topic.Subscribe()
	if err != nil {
		log.Printf("Error subscribing to topic: %v", err)
		return
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

	return stream.Close()
}

func (node *Node) Broadcast(msg *Message) {
	msgToBytes, _ := SerializeMessage(msg)
	if err := node.Topic.Publish(node.C.Ctx, msgToBytes); err != nil {
		fmt.Println("### Broadcast error:", err)
	}
}

// HandleBroadcast processes incoming broadcast (pubsub) messages.
// Broadcast is used for: TxMessage, Propose, Prepare.
func (node *Node) HandleBroadcast() {
	for {
		m, err := node.Subscription.Next(node.C.Ctx)
		if err != nil {
			log.Printf("Subscription error: %v", err)
			return
		}
		msg, err := DeserializeMessage(m.Message.Data)
		if err != nil {
			log.Printf("Error deserializing broadcast: %v", err)
			continue
		}

		switch msg.Type {
		case BlockMessage:
			consensusMsg, err := DeserializeMsg(msg.Payload)
			if err != nil {
				log.Printf("Error decoding consensus message: %v", err)
				continue
			}
			node.routeConsensusMsg(consensusMsg)

		case TxMessage:
			tx := new(blockchain.Transaction)
			if err := tx.Decode(blockchain.NewGobTxDecoder(bytes.NewReader(msg.Payload))); err != nil {
				log.Printf("Error decoding transaction: %v", err)
				continue
			}
			if err := node.Mempool.AddTx(tx); err != nil {
				log.Printf("Rejected transaction: %v", err)
				continue
			}
			log.Printf("Transaction added to mempool (size: %d)", node.Mempool.Len())
		}
	}
}

// --- Transport interface implementation for *Node ---

func (node *Node) Peers() peer.IDSlice {
	return node.Network().Peers()
}

func (node *Node) GetBlockchain() *blockchain.Blockchain {
	return node.Blockchain
}

func (node *Node) GetMempool() *blockchain.Mempool {
	return node.Mempool
}

func (node *Node) GetWallet() wallet.Wallet {
	return node.Wallet
}

func (node *Node) InitBlockchain() {
	log.Printf("Initializing blockchain")
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
