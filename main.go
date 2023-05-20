package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/karimseh/sharehr/network"
	"github.com/multiformats/go-multiaddr"
)

type Config struct {
	Rendezvous    string
	Port          int
	BootstapPeers addrList
}

func main() {
	config := Config{}
	flag.StringVar(&config.Rendezvous, "rendezvous", "sharehr", "")
	flag.IntVar(&config.Port, "port", 0, "")
	flag.Var(&config.BootstapPeers, "peer", "")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	C := network.NewContext(ctx, cancel)
	var bootstrap = false
	if len(config.BootstapPeers) == 0 {
		bootstrap = true
	}
	node, err := network.NewNode(C, config.Port, bootstrap)
	if err != nil {
		log.Fatal(err)
	}
	node.Start(config.Rendezvous, config.BootstapPeers)

}

type addrList []multiaddr.Multiaddr

func (al *addrList) String() string {
	strs := make([]string, len(*al))
	for i, addr := range *al {
		strs[i] = addr.String()
	}
	return strings.Join(strs, ",")
}
func (al *addrList) Set(value string) error {
	addr, err := multiaddr.NewMultiaddr(value)
	if err != nil {
		return err
	}
	*al = append(*al, addr)
	return nil
}
