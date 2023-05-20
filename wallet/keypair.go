package wallet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	"github.com/karimseh/sharehr/types"
)

type PublicKey []byte
type Wallet struct {
	PrivKey *ecdsa.PrivateKey
	PubKey  PublicKey
}

func NewWallet() Wallet {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	if err != nil {
		panic(err)
	}
	return Wallet{
		PrivKey: privKey,
		PubKey:  elliptic.MarshalCompressed(privKey.PublicKey, privKey.PublicKey.X, privKey.PublicKey.Y),
	}
}

func (w Wallet) Sign(data []byte) (*Signature, error) {
	r, s, err := ecdsa.Sign(rand.Reader, w.PrivKey, data)
	if err != nil {
		panic(err)
	}

	return &Signature{
		R: r,
		S: s,
	}, nil
}

func (w Wallet) Address() types.Address {
	h := sha256.Sum256(w.PubKey)

	return types.AddressFromBytes(h[len(h)-20:])
}

type Signature struct {
	S *big.Int
	R *big.Int
}

func (sig Signature) String() string {
	b := append(sig.S.Bytes(), sig.R.Bytes()...)
	return hex.EncodeToString(b)
}

func (sig Signature) Verify(pubKey PublicKey, data []byte) bool {
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), pubKey)
	key := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}

	return ecdsa.Verify(key, data, sig.R, sig.S)
}
