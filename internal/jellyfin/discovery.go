package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
	"syscall"
	"time"

	"mistervision/internal/connection"
)

// Discovery finds Jellyfin servers on directly connected IPv4 networks using
// Jellyfin's UDP port 7359 protocol. It never authenticates or probes a server.
type Discovery struct{}

// Discover collects replies for three seconds, deduplicates server identities,
// and returns a stable name/address order. Cancellation closes the socket.
func (Discovery) Discover(ctx context.Context) ([]connection.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targets, err := discoveryTargets()
	if err != nil {
		return nil, err
	}
	return discoverServers(ctx, targets, 3*time.Second)
}

// discoveryTargets includes each active broadcast network, not loopback or
// point-to-point tunnels. Directed broadcasts cover multiple local interfaces.
func discoveryTargets() ([]*net.UDPAddr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var targets []*net.UDPAddr
	seen := make(map[string]bool)
	for _, device := range interfaces {
		if device.Flags&net.FlagUp == 0 || device.Flags&net.FlagBroadcast == 0 || device.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := device.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			ip := network.IP.To4()
			ones, bits := network.Mask.Size()
			if ip == nil || bits != 32 || ones >= 31 || ones == 0 {
				continue
			}
			broadcast := make(net.IP, 4)
			for i := range broadcast {
				broadcast[i] = ip[i] | ^network.Mask[i]
			}
			if !seen[broadcast.String()] {
				seen[broadcast.String()] = true
				targets = append(targets, &net.UDPAddr{IP: broadcast, Port: 7359})
			}
		}
	}
	return targets, nil
}

// discoverServers accepts explicit destinations for loopback protocol tests.
// Packet and result limits bound work even on a noisy network.
func discoverServers(ctx context.Context, targets []*net.UDPAddr, duration time.Duration) ([]connection.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer socket.Close()
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	raw, err := socket.SyscallConn()
	if err != nil {
		return nil, err
	}
	var optionErr error
	if err = raw.Control(func(fd uintptr) {
		optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return nil, err
	}
	if optionErr != nil {
		return nil, optionErr
	}
	deadline := time.Now().Add(duration)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := socket.SetDeadline(deadline); err != nil {
		return nil, err
	}
	sent := false
	for _, target := range targets {
		if _, err := socket.WriteToUDP([]byte("Who is JellyfinServer?"), target); err == nil {
			sent = true
		}
	}
	if !sent {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("cannot send Jellyfin discovery request")
	}
	servers := make([]connection.Server, 0)
	ids, urls := make(map[string]bool), make(map[string]bool)
	buffer := make([]byte, 4097)
	for count := 0; count < 512 && len(servers) < 64; count++ {
		n, _, err := socket.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				break
			}
			return nil, err
		}
		if n > 4096 {
			continue
		}
		server, ok := parseDiscoveryReply(buffer[:n])
		if !ok || ids[server.ID] || urls[server.URL] {
			continue
		}
		ids[server.ID], urls[server.URL] = true, true
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool {
		a, b := strings.ToLower(servers[i].Name), strings.ToLower(servers[j].Name)
		if a != b {
			return a < b
		}
		return servers[i].URL < servers[j].URL
	})
	return servers, nil
}

func parseDiscoveryReply(data []byte) (connection.Server, bool) {
	var reply struct{ ID, Name, Address string }
	if json.Unmarshal(data, &reply) != nil {
		return connection.Server{}, false
	}
	server := connection.Server{ID: reply.ID, Name: strings.TrimSpace(reply.Name), URL: strings.TrimRight(reply.Address, "/")}
	return server, server.Validate() == nil
}
