package plex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mistervision/internal/connection"
)

// gdmDiscovery finds Plex Media Servers on the local IPv4 network. Replies
// supply addresses only. Account discovery decides access and verifies identity.
// Explicit targets and duration let protocol tests use isolated UDP sockets.
type gdmDiscovery struct {
	targets  []*net.UDPAddr
	duration time.Duration
}

var _ connection.Discoverer = gdmDiscovery{}

// Discover sends anonymous GDM requests and collects a bounded snapshot.
// Cancellation closes the socket immediately. Silence is an empty result.
func (d gdmDiscovery) Discover(ctx context.Context) ([]connection.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targets := d.targets
	if targets == nil {
		var err error
		targets, err = gdmTargets()
		if err != nil {
			return nil, err
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	duration := d.duration
	if duration == 0 {
		duration = 1500 * time.Millisecond
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
		if optionErr == nil {
			optionErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 1)
		}
	}); err != nil {
		return nil, err
	}
	if optionErr != nil {
		return nil, optionErr
	}
	if err := socket.SetDeadline(time.Now().Add(duration)); err != nil {
		return nil, err
	}
	sent := false
	for _, target := range targets {
		if _, err := socket.WriteToUDP([]byte("M-SEARCH * HTTP/1.0\r\n\r\n"), target); err == nil {
			sent = true
		}
	}
	if !sent {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot send Plex LAN discovery request")
	}
	var servers []connection.Server
	seen := make(map[string]bool)
	buffer := make([]byte, 4097)
	for count := 0; count < 512 && len(servers) < 64; count++ {
		n, from, err := socket.ReadFromUDP(buffer)
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
		server, ok := parseGDMReply(buffer[:n], from.IP)
		key := server.ID + "\x00" + server.URL
		if ok && !seen[key] {
			seen[key] = true
			servers = append(servers, server)
		}
	}
	return servers, ctx.Err()
}

// gdmTargets includes Plex's multicast group and each active IPv4 broadcast
// network. Directed broadcasts also cover networks outside the default route.
func gdmTargets() ([]*net.UDPAddr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	targets := []*net.UDPAddr{{IP: net.IPv4(239, 0, 0, 250), Port: 32414}}
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
			if ip == nil || bits != 32 || ones == 0 || ones >= 31 {
				continue
			}
			broadcast := make(net.IP, 4)
			for i := range broadcast {
				broadcast[i] = ip[i] | ^network.Mask[i]
			}
			if !seen[broadcast.String()] {
				seen[broadcast.String()] = true
				targets = append(targets, &net.UDPAddr{IP: broadcast, Port: 32414})
			}
		}
	}
	return targets, nil
}

// parseGDMReply uses the packet's source address, never an advertised Host or
// Location URL. Only Plex server responses with a valid TCP port are candidates.
func parseGDMReply(data []byte, source net.IP) (connection.Server, bool) {
	if len(data) > 4096 || source.To4() == nil || !source.IsGlobalUnicast() && !source.IsLoopback() {
		return connection.Server{}, false
	}
	// Normalize the terminator because some servers omit the final empty line.
	r := textproto.NewReader(bufio.NewReader(strings.NewReader(string(bytes.TrimRight(data, "\r\n")) + "\r\n\r\n")))
	status, err := r.ReadLine()
	if err != nil || status != "HTTP/1.0 200 OK" && status != "HTTP/1.1 200 OK" {
		return connection.Server{}, false
	}
	header, err := r.ReadMIMEHeader()
	if err != nil || header.Get("Content-Type") != "plex/media-server" {
		return connection.Server{}, false
	}
	for _, key := range []string{"Content-Type", "Resource-Identifier", "Name", "Port"} {
		if len(header.Values(key)) != 1 {
			return connection.Server{}, false
		}
	}
	port, err := strconv.Atoi(header.Get("Port"))
	if err != nil || port < 1 || port > 65535 {
		return connection.Server{}, false
	}
	server := connection.Server{ID: header.Get("Resource-Identifier"), Name: header.Get("Name"), URL: "http://" + net.JoinHostPort(source.String(), strconv.Itoa(port))}
	return server, server.Validate() == nil
}
