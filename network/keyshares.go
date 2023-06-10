package network

import (
	"encoding/gob"
	"encoding/json"

	"github.com/coinbase/kryptology/pkg/ted25519/ted25519"
)

type KeyShare struct {
	Key      *ted25519.KeyShare
	Pub      ted25519.PublicKey
	Nonce    *ted25519.NonceShare
	NoncePub ted25519.PublicKey
}

func NewKeyShareMap() map[int]KeyShare {
	config := ted25519.ShareConfiguration{T: 2, N: 4}
	pub, secretShares, _, _ := ted25519.GenerateSharedKey(&config)
	noncePub1, nonceShares1, _, _ := ted25519.GenerateSharedNonce(&config, secretShares[0], pub, []byte{})
	noncePub2, nonceShares2, _, _ := ted25519.GenerateSharedNonce(&config, secretShares[1], pub, []byte{})
	noncePub3, nonceShares3, _, _ := ted25519.GenerateSharedNonce(&config, secretShares[2], pub, []byte{})
	noncePub4, nonceShares4, _, _ := ted25519.GenerateSharedNonce(&config, secretShares[3], pub, []byte{})

	nonceShares := []*ted25519.NonceShare{
		nonceShares1[0].Add(nonceShares2[0]).Add(nonceShares3[0]).Add(nonceShares4[0]),
		nonceShares1[1].Add(nonceShares2[1]).Add(nonceShares3[1]).Add(nonceShares4[1]),
		nonceShares1[2].Add(nonceShares2[2]).Add(nonceShares3[2]).Add(nonceShares4[2]),
		nonceShares1[3].Add(nonceShares2[3]).Add(nonceShares3[3]).Add(nonceShares4[3]),
	}

	noncePub := ted25519.GeAdd(ted25519.GeAdd(noncePub1, noncePub2), ted25519.GeAdd(noncePub3, noncePub4))

	mapShares := make(map[int]KeyShare, 4)
	for i := 0; i < 4; i++ {
		ks := KeyShare{
			Key:      secretShares[i],
			Pub:      pub,
			Nonce:    nonceShares[i],
			NoncePub: noncePub,
		}
		mapShares[i] = ks
	}
	return mapShares
}

func ConvertKeyShareToBytes(keyShare KeyShare) []byte {
	// Convert the struct to bytes
	keyShareBytes, err := json.Marshal(keyShare)
	if err != nil {
		return nil
	}

	return keyShareBytes
}

func DecodeBytesToKeyShare(keyShareBytes []byte) (KeyShare, error) {
	var decodedKeyShare KeyShare

	// Decode the bytes back into a KeyShare struct
	err := json.Unmarshal(keyShareBytes, &decodedKeyShare)
	if err != nil {
		return KeyShare{}, err
	}

	return decodedKeyShare, nil
}

func init() {
	gob.Register(&ted25519.KeyShare{})
	gob.Register(&ted25519.NonceShare{})

}
