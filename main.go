package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"termchat/pkg/network"
	"termchat/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func defaultDeviceName() string {
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		hostname = strings.Split(hostname, ".")[0]
		return hostname
	}
	user := os.Getenv("USER")
	if user != "" {
		return user
	}
	return "User-" + network.GenerateID()[:4]
}

func main() {
	nameFlag := flag.String("name", "", "Display name in chat (default: system hostname)")
	portFlag := flag.Int("port", network.DefaultTCPPort, "TCP port to listen on for peer connections")
	udpFlag := flag.Int("udp", network.DefaultDiscoveryPort, "UDP port for LAN auto-discovery")
	dirFlag := flag.String("dir", "", "Download directory for received files")
	connectFlag := flag.String("connect", "", "Directly connect to a peer address (e.g. 192.168.1.50:7332)")
	roomFlag := flag.String("room", "", "Secret Cloud Room name (e.g. -room secret-squad)")
	passFlag := flag.String("pass", "", "Password / passphrase for AES-256 room encryption")
	relayFlag := flag.String("relay", "wss://termchat-relay.fly.dev/ws", "Cloud Relay WebSocket URL")
	flag.Parse()

	name := *nameFlag
	if name == "" {
		name = defaultDeviceName()
	}

	// 1. Initialize Network Manager
	mgr, err := network.NewManager(name, *portFlag, *udpFlag, *dirFlag, network.NetworkEvents{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start TermChat: %v\n", err)
		os.Exit(1)
	}

	if *passFlag != "" {
		mgr.SetEncryptionPassphrase(*passFlag)
	}

	// 2. Initialize TUI Model
	model := ui.NewModel(mgr)

	// 3. Create Bubble Tea Program (native mouse text selection enabled)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)

	// 4. Bind event bridge from Network Manager to Bubble Tea UI
	events := ui.SetupEventBridge(p)
	mgr.SetEvents(events)

	// 5. Start Network services
	if err := mgr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Network error: %v\n", err)
	}

	// 6. Connect to Cloud Room if specified
	if *roomFlag != "" {
		mgr.ConnectRelay(*relayFlag, *roomFlag)
	}

	// 7. Direct connect if specified in CLI
	if *connectFlag != "" {
		go mgr.ConnectTo(*connectFlag)
	}

	// 8. Run TUI
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}
}
