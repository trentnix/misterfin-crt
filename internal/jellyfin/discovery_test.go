package jellyfin

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryRepliesAreValidatedDeduplicatedAndSorted(t *testing.T) {
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 100)
		n, peer, err := socket.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if string(buffer[:n]) != "Who is JellyfinServer?" {
			t.Error("wrong discovery request")
		}
		for _, reply := range []string{
			`{"Id":"b","Name":"Bedroom","Address":"https://server.example/jellyfin/"}`,
			`{"Id":"a","Name":"Archive","Address":"http://192.0.2.1:8096"}`,
			`{"Id":"a","Name":"Duplicate","Address":"http://192.0.2.2:8096"}`,
			`{"Id":"other","Name":"Same address","Address":"http://192.0.2.1:8096"}`,
			`{"Id":"secret","Name":"Bad","Address":"http://user:password@server"}`,
			`{"Id":"control","Name":"Bad\u001btext","Address":"http://server"}`,
			`{"Id":"missing","Name":"Missing address"}`,
			strings.Repeat("x", 5000), `broken JSON`,
		} {
			_, _ = socket.WriteToUDP([]byte(reply), peer)
		}
	}()
	got, err := discoverServers(t.Context(), []*net.UDPAddr{socket.LocalAddr().(*net.UDPAddr)}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if len(got) != 2 || got[0].ID != "a" || got[1].URL != "https://server.example/jellyfin" {
		t.Fatalf("unexpected servers: %#v", got)
	}
}

func TestDiscoveryCancellationAndEmptyNetwork(t *testing.T) {
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	targets := []*net.UDPAddr{socket.LocalAddr().(*net.UDPAddr)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := discoverServers(ctx, targets, time.Minute); done <- err }()
	buffer := make([]byte, 100)
	_ = socket.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := socket.ReadFromUDP(buffer); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled discovery kept its socket open")
	}
	got, err := discoverServers(t.Context(), targets, 10*time.Millisecond)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty scan: %v %v", got, err)
	}
	got, err = discoverServers(t.Context(), nil, time.Second)
	if err != nil || len(got) != 0 {
		t.Fatalf("no interfaces: %v %v", got, err)
	}
}
