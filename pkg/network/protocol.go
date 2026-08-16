package network

import (
	"encoding/json"
	"time"
)

type MsgType string

const (
	MsgTypeHandshake   MsgType = "handshake"
	MsgTypeChat        MsgType = "chat"
	MsgTypeFileOffer   MsgType = "file_offer"
	MsgTypeFileAccept  MsgType = "file_accept"
	MsgTypeFileReject  MsgType = "file_reject"
	MsgTypeFileChunk   MsgType = "file_chunk"
	MsgTypeFileDone    MsgType = "file_done"
	MsgTypeFileCancel  MsgType = "file_cancel"
	MsgTypePing        MsgType = "ping"
	MsgTypePong        MsgType = "pong"

	// Advanced features
	MsgTypeClipboard   MsgType = "clipboard"
	MsgTypeBatteryReq  MsgType = "battery_req"
	MsgTypeBatteryResp MsgType = "battery_resp"
	MsgTypeNotify      MsgType = "notify"
	MsgTypeRing        MsgType = "ring"
	MsgTypeOpenUrl     MsgType = "open_url"
	MsgTypeMedia       MsgType = "media"
	MsgTypeExecReq     MsgType = "exec_req"
	MsgTypeExecResp    MsgType = "exec_resp"
	MsgTypeEncrypted   MsgType = "encrypted"
)

type Packet struct {
	Type      MsgType   `json:"type"`
	SenderID  string    `json:"sender_id"`
	Sender    string    `json:"sender"`
	Timestamp time.Time `json:"timestamp"`

	// Chat & text payload
	Content string `json:"content,omitempty"`

	// File transfer payload
	FileID     string `json:"file_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	ChunkIndex int    `json:"chunk_index,omitempty"`
	ChunkData  string `json:"chunk_data,omitempty"` // base64 encoded
	IsLast     bool   `json:"is_last,omitempty"`
	Error      string `json:"error,omitempty"`

	// Device action payload
	Action    string `json:"action,omitempty"`
	URL       string `json:"url,omitempty"`
	ExtraData string `json:"extra_data,omitempty"`
}

func EncodePacket(p *Packet) ([]byte, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func DecodePacket(data []byte) (*Packet, error) {
	var p Packet
	err := json.Unmarshal(data, &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
