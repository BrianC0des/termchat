package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"termchat/pkg/network"
	"termchat/pkg/system"
	"termchat/pkg/ui"
	"termchat/pkg/workspace"

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
	ttlFlag := flag.String("ttl", "", "Room auto-expire self-destruct duration (e.g. -ttl 1h, -ttl 30m)")
	autoDeleteFlag := flag.String("autodelete", "", "Ephemeral disappearing messages TTL (e.g. -autodelete 5m, -autodelete 30s)")
	relayFlag := flag.String("relay", "wss://termchat-o51d.onrender.com/ws", "Cloud Relay WebSocket URL")
	updateFlag := flag.Bool("update", false, "Self-update TermChat to the latest release")
	versionFlag := flag.Bool("version", false, "Show TermChat version")
	vFlag := flag.Bool("v", false, "Show TermChat version")
	initFlag := flag.Bool("init", false, "Initialize a .termchat/room.json project collab room in the current directory")
	flag.Parse()

	if *versionFlag || *vFlag {
		fmt.Printf("TermChat %s (%s/%s)\n", system.AppVersion, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Handle 'termchat init [room-name]' or -init flag
	if *initFlag || (len(os.Args) > 1 && os.Args[1] == "init") {
		roomName := ""
		if len(os.Args) > 2 && os.Args[1] == "init" {
			roomName = os.Args[2]
		}
		wsCfg, path, err := workspace.InitWorkspace("", "", roomName, *passFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Failed to initialize project room: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n  ╔═══════════════════════════════════════════════════════╗\n")
		fmt.Printf("  ║   🐙 Project Collab Room Initialized Successfully!    ║\n")
		fmt.Printf("  ╚═══════════════════════════════════════════════════════╝\n\n")
		fmt.Printf("  • Config File: %s\n", path)
		if wsCfg.Repo != "" {
			fmt.Printf("  • Repository:  %s\n", wsCfg.Repo)
		}
		fmt.Printf("  • Collab Room: %s\n", wsCfg.Room)
		fmt.Printf("\n  👉 Commit .termchat/room.json to git so collaborators auto-join on 'git clone'!\n\n")
		os.Exit(0)
	}

	if *updateFlag {
		msg, err := system.UpdateSelfWithProgress(func(progressMsg string) {
			fmt.Println(progressMsg)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Update failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(msg)
		os.Exit(0)
	}

	cfg := system.LoadConfig()
	name := *nameFlag
	if name == "" {
		if cfg.Nickname != "" {
			name = cfg.Nickname
		} else {
			name = defaultDeviceName()
		}
	} else {
		cfg.Nickname = name
		system.SaveConfig(cfg)
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
	if *ttlFlag != "" {
		if d, err := time.ParseDuration(*ttlFlag); err == nil && d > 0 {
			model.SetRoomTTL(d)
		}
	}
	if *autoDeleteFlag != "" {
		if d, err := time.ParseDuration(*autoDeleteFlag); err == nil && d > 0 {
			model.SetAutoDeleteTTL(d)
		}
	}

	// 3. Create Bubble Tea Program (native mouse wheel & text selection enabled)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// 4. Bind event bridge from Network Manager to Bubble Tea UI
	events := ui.SetupEventBridge(p)
	mgr.SetEvents(events)

	// 5. Start Network services
	if err := mgr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Network error: %v\n", err)
	}

	// 6. Connect to Cloud Room or Auto-Join Project Workspace
	if *roomFlag != "" {
		mgr.ConnectRelay(*relayFlag, *roomFlag)
	} else if wsCfg, _, err := workspace.FindWorkspace(""); err == nil && wsCfg.AutoConnect && wsCfg.Room != "" {
		relayURL := *relayFlag
		if wsCfg.Relay != "" && *relayFlag == "wss://termchat-o51d.onrender.com/ws" {
			relayURL = wsCfg.Relay
		}
		if *passFlag == "" && wsCfg.Passphrase != "" {
			mgr.SetEncryptionPassphrase(wsCfg.Passphrase)
		}
		mgr.ConnectRelay(relayURL, wsCfg.Room)
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
