package network

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/karimseh/sharehr/wallet"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	discovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
)

type Context struct {
	Ctx    context.Context
	Cancel func()
}

type Node struct {
	host.Host
	C         Context
	Dht       *dht.IpfsDHT
	Bootstrap bool

	Wallet wallet.Wallet
	//BC blockchain
	//Mempool
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
	ticker := time.NewTicker(time.Second * 60)
	defer ticker.Stop()

	for {
		select {
		case <-node.C.Ctx.Done():
			return
		case <-ticker.C:
			log.Printf("Rotuing Discovery ...")
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
					node.SendMessageToPeer(p.ID, &Message{
						Type:    BlockMessage,
						Payload: []byte("THIS IS A TEST!"),
					})
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
	node.ConnectToBootsrap(bootstrapPeers)
	go node.Discover(rendezvous)

	go node.startListening()

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
	log.Println(msg)
	// Close the stream
	_ = stream.Close()
}

func (node *Node) startListening() {
	node.SetStreamHandler("sharehr", node.handleStream)
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
