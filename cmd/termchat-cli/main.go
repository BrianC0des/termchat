package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"termchat/pkg/network"
	"termchat/pkg/system"
)

func generateID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func discoverPeer(udpPort int, timeout time.Duration) (string, error) {
	// Check environment variable first
	if envPeer := os.Getenv("TERMCHAT_PEER"); envPeer != "" {
		if !strings.Contains(envPeer, ":") {
			envPeer = fmt.Sprintf("%s:%d", envPeer, network.DefaultTCPPort)
		}
		return envPeer, nil
	}

	listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 2048)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}

		var beacon network.Beacon
		if err := json.Unmarshal(buf[:n], &beacon); err != nil {
			continue
		}

		return fmt.Sprintf("%s:%d", remoteAddr.IP.String(), beacon.TCPPort), nil
	}

	// Fallback to localhost / local subnet guess if not discovered
	return "192.168.1.13:7332", nil
}

func connectToPeer(peerAddr string) (net.Conn, *bufio.Reader, *bufio.Writer, error) {
	conn, err := net.DialTimeout("tcp", peerAddr, 4*time.Second)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not connect to phone at %s: %w", peerAddr, err)
	}

	writer := bufio.NewWriter(conn)
	reader := bufio.NewReader(conn)

	// Send handshake
	handshake := &network.Packet{
		Type:      network.MsgTypeHandshake,
		SenderID:  "agy-cli-" + generateID()[:6],
		Sender:    "Antigravity",
		Timestamp: time.Now(),
	}

	data, _ := network.EncodePacket(handshake)
	_, err = writer.Write(data)
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	_ = writer.Flush()

	return conn, reader, writer, nil
}

func main() {
	peerFlag := flag.String("peer", "", "Phone IP:Port (default: auto-discover or $TERMCHAT_PEER)")
	timeoutFlag := flag.Duration("timeout", 30*time.Second, "Command execution timeout")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	action := strings.ToLower(args[0])

	peerAddr := *peerFlag
	if peerAddr == "" {
		var err error
		peerAddr, err = discoverPeer(network.DefaultDiscoveryPort, 1500*time.Millisecond)
		if err != nil || peerAddr == "" {
			peerAddr = "192.168.1.13:7332"
		}
	}

	conn, reader, writer, err := connectToPeer(peerAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Connection Error: %v\n(Make sure TermChat is running in Termux on your phone!)\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	switch action {
	case "exec", "sh", "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: termchat-cli exec \"<command>\"")
			os.Exit(1)
		}
		cmdToRun := strings.Join(args[1:], " ")
		runRemoteExec(conn, reader, writer, cmdToRun, *timeoutFlag)

	case "battery", "batt":
		getRemoteBattery(conn, reader, writer, *timeoutFlag)

	case "send", "upload":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: termchat-cli send <file_path>")
			os.Exit(1)
		}
		filePath := args[1]
		sendRemoteFile(writer, filePath)

	case "notify":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: termchat-cli notify \"<message>\"")
			os.Exit(1)
		}
		msg := strings.Join(args[1:], " ")
		sendNotification(writer, msg)

	case "ring", "vibrate", "find":
		sendRing(writer)

	case "open", "url":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: termchat-cli open <url>")
			os.Exit(1)
		}
		openURL(writer, args[1])

	case "clip":
		if len(args) < 2 {
			// Read from stdin or local clipboard
			clip, err := system.ReadClipboard()
			if err != nil || clip == "" {
				fmt.Fprintln(os.Stderr, "Clipboard is empty")
				os.Exit(1)
			}
			sendClip(writer, clip)
		} else {
			sendClip(writer, strings.Join(args[1:], " "))
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command '%s'\n", action)
		printUsage()
		os.Exit(1)
	}
}

func runRemoteExec(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer, cmdStr string, timeout time.Duration) {
	req := &network.Packet{
		Type:      network.MsgTypeExecReq,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
		Content:   cmdStr,
	}

	data, _ := network.EncodePacket(req)
	_, _ = writer.Write(data)
	_ = writer.Flush()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error waiting for response: %v\n", err)
			os.Exit(1)
		}

		pkt, err := network.DecodePacket(line)
		if err != nil {
			continue
		}

		if pkt.Type == network.MsgTypeExecResp {
			if pkt.Error == "true" {
				fmt.Fprintln(os.Stderr, pkt.Content)
				os.Exit(1)
			} else {
				fmt.Print(pkt.Content)
				if !strings.HasSuffix(pkt.Content, "\n") {
					fmt.Println()
				}
				os.Exit(0)
			}
		}
	}
}

func getRemoteBattery(conn net.Conn, reader *bufio.Reader, writer *bufio.Writer, timeout time.Duration) {
	req := &network.Packet{
		Type:      network.MsgTypeBatteryReq,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
	}

	data, _ := network.EncodePacket(req)
	_, _ = writer.Write(data)
	_ = writer.Flush()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
			os.Exit(1)
		}

		pkt, err := network.DecodePacket(line)
		if err != nil {
			continue
		}

		if pkt.Type == network.MsgTypeBatteryResp {
			var info system.BatteryInfo
			if err := json.Unmarshal([]byte(pkt.ExtraData), &info); err == nil {
				fmt.Printf("🔋 Battery: %d%%\n⚡ Status: %s\n🔌 Plugged: %s\n", info.Percentage, info.Status, info.Plugged)
			} else {
				fmt.Println(pkt.ExtraData)
			}
			os.Exit(0)
		}
	}
}

func sendRemoteFile(writer *bufio.Writer, filePath string) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open file: %v\n", err)
		os.Exit(1)
	}

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	fileID := generateID()
	fileName := filepath.Base(filePath)
	fileSize := fileInfo.Size()

	offer := &network.Packet{
		Type:      network.MsgTypeFileOffer,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
		FileID:    fileID,
		FileName:  fileName,
		FileSize:  fileSize,
	}

	data, _ := network.EncodePacket(offer)
	_, _ = writer.Write(data)
	_ = writer.Flush()

	buf := make([]byte, network.ChunkSize)
	chunkIdx := 0

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunkBase64 := base64.StdEncoding.EncodeToString(buf[:n])
			isLast := (readErr == io.EOF)

			chunkPkt := &network.Packet{
				Type:       network.MsgTypeFileChunk,
				SenderID:   "agy",
				Sender:     "Antigravity",
				FileID:     fileID,
				ChunkIndex: chunkIdx,
				ChunkData:  chunkBase64,
				IsLast:     isLast,
			}

			cData, _ := network.EncodePacket(chunkPkt)
			_, _ = writer.Write(cData)
			_ = writer.Flush()
			chunkIdx++
			time.Sleep(2 * time.Millisecond)
		}

		if readErr != nil {
			break
		}
	}

	donePkt := &network.Packet{
		Type:      network.MsgTypeFileDone,
		SenderID:  "agy",
		Sender:    "Antigravity",
		FileID:    fileID,
		FileName:  fileName,
		FileSize:  fileSize,
	}
	dData, _ := network.EncodePacket(donePkt)
	_, _ = writer.Write(dData)
	_ = writer.Flush()

	fmt.Printf("✅ Sent '%s' (%s) to phone!\n", fileName, network.FormatBytes(fileSize))
}

func sendNotification(writer *bufio.Writer, msg string) {
	p := &network.Packet{
		Type:      network.MsgTypeNotify,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
		Content:   msg,
	}
	data, _ := network.EncodePacket(p)
	_, _ = writer.Write(data)
	_ = writer.Flush()
	fmt.Println("🔔 Notification pushed to phone screen!")
}

func sendRing(writer *bufio.Writer) {
	p := &network.Packet{
		Type:      network.MsgTypeRing,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
	}
	data, _ := network.EncodePacket(p)
	_, _ = writer.Write(data)
	_ = writer.Flush()
	fmt.Println("🔔 Alert triggered on phone!")
}

func openURL(writer *bufio.Writer, url string) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	p := &network.Packet{
		Type:      network.MsgTypeOpenUrl,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
		URL:       url,
	}
	data, _ := network.EncodePacket(p)
	_, _ = writer.Write(data)
	_ = writer.Flush()
	fmt.Printf("🌐 URL '%s' opened on phone!\n", url)
}

func sendClip(writer *bufio.Writer, text string) {
	p := &network.Packet{
		Type:      network.MsgTypeClipboard,
		SenderID:  "agy",
		Sender:    "Antigravity",
		Timestamp: time.Now(),
		Content:   text,
	}
	data, _ := network.EncodePacket(p)
	_, _ = writer.Write(data)
	_ = writer.Flush()
	fmt.Printf("📋 Synced %d characters to phone clipboard!\n", len(text))
}

func printUsage() {
	fmt.Println(`⚡ termchat-cli — Antigravity & Scriptable Phone Bridge

Usage:
  termchat-cli exec "<command>"   Run shell command in phone's Termux
  termchat-cli battery            Get phone battery percentage & status
  termchat-cli send <file_path>   Push file to phone's storage
  termchat-cli notify "<message>" Send push notification to phone screen
  termchat-cli ring               Vibrate / ring phone
  termchat-cli open <url>         Open URL on phone's browser
  termchat-cli clip "<text>"      Set phone's clipboard

Options:
  -peer <ip:port>                 Phone IP (default: auto-discover or $TERMCHAT_PEER)
  -timeout <duration>             Command timeout (default: 30s)`)
}
