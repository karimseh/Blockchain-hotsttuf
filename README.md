#  HotStuff-2 Blockchain 

A blockchain prototype implementation using the [HotStuff-2](https://eprint.iacr.org/2023/397) two-phase BFT consensus protocol (Malkhi & Nayak). Built for my academic research on secure Electronic Health Record (EHR) management.



### Consensus Protocol

HotStuff-2 achieves optimal **two-phase** responsive BFT consensus:

1. **Propose** : Leader broadcasts block with highest QC and double cert
2. **Vote** : Parties vote if QC ≥ locked cert; commit from double cert
3. **Prepare**: Leader aggregates 2t+1 votes into QC `C_v(B_k)`
4. **Vote2**: Parties confirm QC, send vote2 to next leader
5. **Commit** : Next leader forms double cert `C_v(C_v(B_k))` → block committed

The commit rule requires a **double certificate** (a certificate on a certificate), reducing the commit path from 3 phases (PBFT/HotStuff-1) to 2.

### P2P Networking

- **libp2p** with GossipSub (broadcasts) and direct streams (unicast)
- **Kademlia DHT** for peer discovery
- **Threshold signatures**: ted25519 (3-of-4 scheme via Coinbase Kryptology)

## Configuration

| Parameter | Value | Description |
|-----------|-------|-------------|
| N | 4 | Total parties |
| t | 1 | Max Byzantine faults |
| Quorum | 3 | Votes for certificate (2t+1) |
| Δ | 2s | Network delay bound |
| Block interval | 2s | Minimum time between proposals |

## Running

### Docker (recommended)

```bash
docker compose up --build
```

Starts a 4-node network (1 bootstrap + 3 nodes) on a bridge network.

### Local

```bash
# Bootstrap node
go run . --port 4001 --rendezvous sharehr

# Other nodes (use bootstrap's multiaddr)
go run . --port 4002 --rendezvous sharehr --peer /ip4/127.0.0.1/tcp/4001/p2p/<BOOTSTRAP_PEER_ID>
```


## References

- Dahlia Malkhi, Kartik Nayak. *"HotStuff-2: Optimal Two-Phase Responsive BFT"*, 2023. [ePrint 2023/397](https://eprint.iacr.org/2023/397)
