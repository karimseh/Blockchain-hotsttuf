package network

import "encoding/json"

type MessageType int

const (
	TxMessaege MessageType = iota
	BlockMessage
	GetDataMessage
)

type Message struct {
	Type    MessageType
	Payload []byte
}

func SerializeMessage(msg *Message) ([]byte, error) {
	return json.Marshal(msg)
}

func DeserializeMessage(data []byte) (*Message, error) {
	msg := &Message{}
	err := json.Unmarshal(data, msg)
	return msg, err
}

// establish a way to comunicate (send messages)
// sync blockchain
// propagate Tx
// propagate Block
