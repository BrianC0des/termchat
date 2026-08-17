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
	MsgTypeEncrypted   MsgType = "encrypted"
	MsgTypeStatus      MsgType = "status"
	MsgTypeTopic       MsgType = "topic"
	MsgTypePin         MsgType = "pin"
	MsgTypeDestroy     MsgType = "room_destroy"
)

type Packet struct {
	Type      MsgType   `json:"type"`
	Room      string    `json:"room,omitempty"`
	SenderID  string    `json:"sender_id"`
	Sender    string    `json:"sender"`
	Timestamp time.Time `json:"timestamp"`

	// Chat & text payload
	Content string `json:"content,omitempty"`

	// Reply payload
	ReplyToNum    int    `json:"reply_to_num,omitempty"`
	ReplyToSender string `json:"reply_to_sender,omitempty"`
	ReplyToText   string `json:"reply_to_text,omitempty"`

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

func GetFileIcon(fileName string) string {
	ext := ""
	for i := len(fileName) - 1; i >= 0; i-- {
		if fileName[i] == '.' {
			ext = fileName[i:]
			break
		}
	}
	switch ext {
	case ".zip", ".tar", ".gz", ".tgz", ".rar", ".7z", ".bz2", ".xz":
		return "[ZIP]"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".bmp":
		return "[IMG]"
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv":
		return "[VID]"
	case ".mp3", ".wav", ".ogg", ".flac", ".m4a", ".aac":
		return "[AUD]"
	case ".pdf":
		return "[PDF]"
	case ".doc", ".docx", ".odt", ".rtf":
		return "[DOC]"
	case ".xls", ".xlsx", ".csv", ".ods":
		return "[CSV]"
	case ".ppt", ".pptx", ".odp":
		return "[PPT]"
	case ".go", ".rs", ".py", ".js", ".ts", ".jsx", ".tsx", ".html", ".css", ".c", ".cpp", ".h", ".json", ".yaml", ".yml", ".sh", ".bash", ".sql":
		return "[CODE]"
	case ".apk", ".aab":
		return "[APK]"
	case ".exe", ".msi", ".dmg", ".pkg", ".deb", ".rpm", ".AppImage":
		return "[EXE]"
	case ".iso", ".img":
		return "[ISO]"
	default:
		return "[FILE]"
	}
}
