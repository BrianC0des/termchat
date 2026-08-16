package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	DefaultDiscoveryPort = 7331
	BeaconInterval       = 2 * time.Second
	PeerTimeout          = 10 * time.Second
)

type Beacon struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TCPPort   int    `json:"tcp_port"`
	Timestamp int64  `json:"timestamp"`
}

type DiscoveredPeer struct {
	ID       string
	Name     string
	IP       net.IP
	TCPPort  int
	LastSeen time.Time
}

func (p DiscoveredPeer) Addr() string {
	return fmt.Sprintf("%s:%d", p.IP.String(), p.TCPPort)
}

type DiscoveryService struct {
	localID    string
	localName  string
	tcpPort    int
	udpPort    int
	peers      map[string]DiscoveredPeer
	peersMutex sync.RWMutex
	onPeer     func(peer DiscoveredPeer)
	onPeerLost func(peerID string)
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewDiscoveryService(localID, localName string, tcpPort, udpPort int, onPeer func(DiscoveredPeer), onPeerLost func(string)) *DiscoveryService {
	ctx, cancel := context.WithCancel(context.Background())
	return &DiscoveryService{
		localID:    localID,
		localName:  localName,
		tcpPort:    tcpPort,
		udpPort:    udpPort,
		peers:      make(map[string]DiscoveredPeer),
		onPeer:     onPeer,
		onPeerLost: onPeerLost,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (d *DiscoveryService) UpdateName(name string) {
	d.peersMutex.Lock()
	d.localName = name
	d.peersMutex.Unlock()
}

func (d *DiscoveryService) Start() error {
	// Listen for incoming UDP beacons
	listenAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: d.udpPort,
	}

	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP %d: %w", d.udpPort, err)
	}

	go d.listenLoop(conn)
	go d.broadcastLoop()
	go d.cleanupLoop()

	return nil
}

func (d *DiscoveryService) Stop() {
	d.cancel()
}

func (d *DiscoveryService) listenLoop(conn *net.UDPConn) {
	defer conn.Close()
	buf := make([]byte, 2048)

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		var beacon Beacon
		if err := json.Unmarshal(buf[:n], &beacon); err != nil {
			continue
		}

		if beacon.ID == d.localID {
			continue // Skip self beacon
		}

		peer := DiscoveredPeer{
			ID:       beacon.ID,
			Name:     beacon.Name,
			IP:       remoteAddr.IP,
			TCPPort:  beacon.TCPPort,
			LastSeen: time.Now(),
		}

		d.peersMutex.Lock()
		_, exists := d.peers[peer.ID]
		d.peers[peer.ID] = peer
		d.peersMutex.Unlock()

		if d.onPeer != nil {
			d.onPeer(peer)
		}
		_ = exists
	}
}

func (d *DiscoveryService) broadcastLoop() {
	ticker := time.NewTicker(BeaconInterval)
	defer ticker.Stop()

	// Initial broadcast immediately
	d.broadcastOnce()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.broadcastOnce()
		}
	}
}

func (d *DiscoveryService) broadcastOnce() {
	d.peersMutex.RLock()
	name := d.localName
	d.peersMutex.RUnlock()

	beacon := Beacon{
		ID:        d.localID,
		Name:      name,
		TCPPort:   d.tcpPort,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(beacon)
	if err != nil {
		return
	}

	broadcastAddrs := getBroadcastAddresses(d.udpPort)
	for _, addr := range broadcastAddrs {
		conn, err := net.DialUDP("udp4", nil, addr)
		if err == nil {
			_, _ = conn.Write(data)
			conn.Close()
		}
	}
}

func (d *DiscoveryService) cleanupLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.peersMutex.Lock()
			now := time.Now()
			for id, peer := range d.peers {
				if now.Sub(peer.LastSeen) > PeerTimeout {
					delete(d.peers, id)
					if d.onPeerLost != nil {
						go d.onPeerLost(id)
					}
				}
			}
			d.peersMutex.Unlock()
		}
	}
}

func getBroadcastAddresses(port int) []*net.UDPAddr {
	var addrs []*net.UDPAddr
	// Global broadcast fallback
	addrs = append(addrs, &net.UDPAddr{IP: net.IPv4bcast, Port: port})

	ifaces, err := net.Interfaces()
	if err != nil {
		return addrs
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range ifAddrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}

			ip := ipNet.IP.To4()
			mask := ipNet.Mask
			if len(mask) == 4 {
				bcast := net.IPv4(
					ip[0]|^mask[0],
					ip[1]|^mask[1],
					ip[2]|^mask[2],
					ip[3]|^mask[3],
				)
				addrs = append(addrs, &net.UDPAddr{IP: bcast, Port: port})
			}
		}
	}

	return addrs
}

func GetLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}
	return ips
}
