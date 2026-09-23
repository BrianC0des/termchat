package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"context"
	"termchat/pkg/ghauth"
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
	lanFlag := flag.Bool("lan", false, "Initialize or connect in offline Local LAN mode")
	flag.Parse()

	// Fall back to the TERMCHAT_PASS environment variable when -pass is not
	// given on the command line, so the passphrase does not have to be
	// typed directly into the shell history / process list via -pass.
	if *passFlag == "" {
		if envPass := os.Getenv("TERMCHAT_PASS"); envPass != "" {
			*passFlag = envPass
		}
	}

	if *versionFlag || *vFlag {
		fmt.Printf("TermChat %s (%s/%s)\n", system.AppVersion, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Handle 'termchat login' / 'termchat auth login'
	if len(os.Args) > 1 && (os.Args[1] == "login" || (os.Args[1] == "auth" && len(os.Args) > 2 && os.Args[2] == "login")) {
		fmt.Println("◆ GITHUB DEVICE AUTHORIZATION")
		client := &ghauth.Client{}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		user, err := client.Login(ctx, func(dc *ghauth.DeviceCodeResponse) {
			fmt.Printf("\n  1. First copy your one-time code: \033[1;32m%s\033[0m\n", dc.UserCode)
			fmt.Printf("  2. Open: \033[1;34m%s\033[0m\n\n", dc.VerificationURI)
			fmt.Println("Waiting for browser authorization...")
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[ERR] GitHub authentication failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n✓ Successfully authenticated as @%s!\nCredentials saved to ~/.config/termchat/hosts.json (0600)\n", user.Login)
		os.Exit(0)
	}

	// Handle 'termchat logout' / 'termchat auth logout'
	if len(os.Args) > 1 && (os.Args[1] == "logout" || (os.Args[1] == "auth" && len(os.Args) > 2 && os.Args[2] == "logout")) {
		if err := ghauth.ClearToken(); err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Failed to log out: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Logged out of TermChat GitHub session.")
		os.Exit(0)
	}

	// Handle 'termchat whoami' / 'termchat auth status'
	if len(os.Args) > 1 && (os.Args[1] == "whoami" || (os.Args[1] == "auth" && len(os.Args) > 2 && os.Args[2] == "status")) {
		res, err := ghauth.GetToken()
		if err != nil {
			fmt.Println("Not logged in to GitHub. (Run 'termchat login' to authenticate)")
			os.Exit(0)
		}
		client := &ghauth.Client{}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u, err := client.FetchUser(ctx, res.Token)
		if err != nil {
			fmt.Printf("✓ Token present (source: %s), but failed to fetch profile: %v\n", res.Source, err)
			os.Exit(1)
		}
		fmt.Printf("✓ Logged in to github.com as @%s (via %s)\n", u.Login, res.Source)
		if u.Name != "" {
			fmt.Printf("  • Name:   %s\n", u.Name)
		}
		if u.Email != "" {
			fmt.Printf("  • Email:  %s\n", u.Email)
		}
		os.Exit(0)
	}

	// Handle 'termchat delta <old-binary> <new-binary> [output-patch]'
	if len(os.Args) >= 4 && os.Args[1] == "delta" {
		oldPath := os.Args[2]
		newPath := os.Args[3]
		outPath := ""
		if len(os.Args) >= 5 {
			outPath = os.Args[4]
		} else {
			outPath = newPath + ".delta.zst"
		}

		oldBytes, err := os.ReadFile(oldPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Reading old binary: %v\n", err)
			os.Exit(1)
		}
		newBytes, err := os.ReadFile(newPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Reading new binary: %v\n", err)
			os.Exit(1)
		}

		patch, err := system.GenerateDelta(oldBytes, newBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Generating delta patch: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(outPath, patch, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Writing delta patch: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("[OK] Binary delta patch generated: %s (%.1f KB, %.1f%% of full binary size)\n",
			outPath, float64(len(patch))/1024, float64(len(patch))/float64(len(newBytes))*100)
		os.Exit(0)
	}

	// Handle 'termchat init [room-name]' or -init flag
	if *initFlag || (len(os.Args) > 1 && os.Args[1] == "init") {
		cfg := system.LoadConfig()
		roomName := ""
		for _, arg := range os.Args[2:] {
			if !strings.HasPrefix(arg, "-") && roomName == "" {
				roomName = arg
			}
			if arg == "--lan" || arg == "-lan" {
				*lanFlag = true
			}
		}
		ident, _ := system.GetOrCreateIdentity()
		fp := ""
		if ident != nil {
			fp = ident.Fingerprint()
		}
		wsCfg, path, err := workspace.InitWorkspace("", "", roomName, *passFlag, fp, cfg.Nickname)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERR] Failed to initialize project room: %v\n", err)
			os.Exit(1)
		}
		if *lanFlag {
			wsCfg.Relay = "lan"
			if err := workspace.SaveConfig(path, wsCfg); err != nil {
				fmt.Fprintf(os.Stderr, "[ERR] Failed to save room config: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Printf("\n  ╔═══════════════════════════════════════════════════════╗\n")
		fmt.Printf("  ║      Project Collab Room Initialized Successfully     ║\n")
		fmt.Printf("  ╚═══════════════════════════════════════════════════════╝\n\n")
		fmt.Printf("  • Config File: %s\n", path)
		if wsCfg.Repo != "" {
			fmt.Printf("  • Repository:  %s\n", wsCfg.Repo)
		}
		fmt.Printf("  • Collab Room: %s\n", wsCfg.Room)
		if wsCfg.Relay == "lan" {
			fmt.Printf("  • Mode:        Offline Local LAN P2P\n")
		} else {
			fmt.Printf("  • Mode:        Cloud Relay (24/7 Global)\n")
		}
		fmt.Printf("\n  • Commit .termchat/room.json to git so collaborators auto-join on 'git clone'!\n")
		if *passFlag != "" {
			fmt.Printf("  • Your passphrase was saved locally to .termchat/secret.local.json (gitignored) — it is NOT committed.\n")
			fmt.Printf("    Share it with teammates over a separate, trusted channel (not in the repo).\n\n")
		} else {
			fmt.Printf("\n")
		}
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
		if authUser, _ := ghauth.GetAuthenticatedUser(); authUser != "" {
			name = authUser
		} else if res, err := ghauth.GetToken(); err == nil && res.Token != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			client := &ghauth.Client{}
			if u, err := client.FetchUser(ctx, res.Token); err == nil && u.Login != "" {
				name = u.Login
			}
			cancel()
		}
	}
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
		os.Exit(1)
	}

	// 6. Connect to Cloud Room or Auto-Join Project Workspace
	if *roomFlag != "" {
		model.SwitchRoomHistory(*roomFlag)
		mgr.ConnectRelay(*relayFlag, *roomFlag)
	} else if wsCfg, _, err := workspace.FindWorkspace(""); err == nil && wsCfg.AutoConnect && wsCfg.Room != "" {
		model.SwitchRoomHistory(wsCfg.Room)
		if *passFlag == "" && wsCfg.Passphrase != "" {
			mgr.SetEncryptionPassphrase(wsCfg.Passphrase)
		}
		if !*lanFlag && strings.ToLower(wsCfg.Relay) != "lan" && strings.ToLower(wsCfg.Relay) != "local" && wsCfg.Relay != "off" {
			relayURL := *relayFlag
			if wsCfg.Relay != "" && *relayFlag == "wss://termchat-o51d.onrender.com/ws" {
				relayURL = wsCfg.Relay
			}
			mgr.ConnectRelay(relayURL, wsCfg.Room)
		}
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
