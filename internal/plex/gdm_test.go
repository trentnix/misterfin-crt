package plex

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

const gdmReply = "HTTP/1.0 200 OK\r\nContent-Type: plex/media-server\r\nResource-Identifier: home\r\nName: Home Plex\r\nPort: 32400\r\n"

func TestParseGDMReply(t *testing.T) {
	for _, test := range []struct {
		name, packet string
		valid        bool
	}{
		{"server", gdmReply, true},
		{"LF headers", strings.ReplaceAll(gdmReply, "\r\n", "\n"), true},
		{"case insensitive", strings.ReplaceAll(gdmReply, "Port:", "port:"), true},
		{"untrusted host", gdmReply + "Host: attacker.example\r\nLocation: http://attacker.example/\r\n", true},
		{"player", strings.ReplaceAll(gdmReply, "media-server", "media-player"), false},
		{"status", strings.ReplaceAll(gdmReply, "200 OK", "500 Error"), false},
		{"missing identity", strings.ReplaceAll(gdmReply, "Resource-Identifier: home\r\n", ""), false},
		{"duplicate port", gdmReply + "Port: 80\r\n", false},
		{"zero port", strings.ReplaceAll(gdmReply, "32400", "0"), false},
		{"large port", strings.ReplaceAll(gdmReply, "32400", "65536"), false},
		{"invalid port", strings.ReplaceAll(gdmReply, "32400", "80/path"), false},
		{"control text", strings.ReplaceAll(gdmReply, "Home Plex", "Home\x00Plex"), false},
		{"oversized", gdmReply + strings.Repeat("x", 4096), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, ok := parseGDMReply([]byte(test.packet), net.ParseIP("192.168.1.100"))
			if ok != test.valid {
				t.Fatalf("accepted=%t", ok)
			}
			if ok && (server.ID != "home" || server.URL != "http://192.168.1.100:32400") {
				t.Fatalf("unexpected candidate: %v", server)
			}
		})
	}
	for _, source := range []string{"0.0.0.0", "255.255.255.255", "239.0.0.250", "::1"} {
		if _, ok := parseGDMReply([]byte(gdmReply), net.ParseIP(source)); ok {
			t.Fatalf("accepted source %s", source)
		}
	}
}

// gdmResponder keeps protocol tests off the LAN and confirms that discovery
// requests contain no account information or credentials.
func gdmResponder(t *testing.T, respond bool) (gdmDiscovery, <-chan struct{}) {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.Close() })
	seen := make(chan struct{})
	go func() {
		buffer := make([]byte, 4096)
		n, from, err := socket.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		defer close(seen)
		if string(buffer[:n]) != "M-SEARCH * HTTP/1.0\r\n\r\n" {
			t.Error("unexpected discovery request")
		}
		if respond {
			for _, packet := range []string{"invalid", gdmReply, gdmReply, strings.ReplaceAll(gdmReply, "32400", "32401")} {
				if _, err := socket.WriteToUDP([]byte(packet), from); err != nil {
					t.Error(err)
				}
			}
		}
	}()
	return gdmDiscovery{targets: []*net.UDPAddr{socket.LocalAddr().(*net.UDPAddr)}, duration: 50 * time.Millisecond}, seen
}

func TestGDMCollectsAndDeduplicatesEndpoints(t *testing.T) {
	d, _ := gdmResponder(t, true)
	servers, err := d.Discover(t.Context())
	if err != nil || len(servers) != 2 {
		t.Fatalf("servers=%v err=%v", servers, err)
	}
	if servers[0].URL != "http://127.0.0.1:32400" || servers[1].URL != "http://127.0.0.1:32401" {
		t.Fatal(servers)
	}
}

func TestGDMSilenceAndCancellation(t *testing.T) {
	d, seen := gdmResponder(t, false)
	if servers, err := d.Discover(t.Context()); err != nil || len(servers) != 0 {
		t.Fatalf("servers=%v err=%v", servers, err)
	}
	<-seen
	d, seen = gdmResponder(t, false)
	d.duration = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := d.Discover(ctx); done <- err }()
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation left the socket open")
	}
	if _, err := d.Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
