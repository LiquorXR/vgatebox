package main

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestUDPPortAvailability(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if portAvailableUDP(port) {
		t.Fatalf("UDP port %d reported free while bound", port)
	}
}

func TestParseUDPReqHeaderIPv4(t *testing.T) {
	b := []byte{0, 0, 0, atypIPv4, 8, 8, 8, 8, 0, 53, 1, 2, 3}
	atyp, addr, host, port, payload, ok := parseUDPReqHeader(b)
	if !ok || atyp != atypIPv4 || host != "8.8.8.8" || port != 53 || len(payload) != 3 || payload[0] != 1 {
		t.Fatalf("bad parse: ok=%v atyp=%v host=%v port=%v payload=%v", ok, atyp, host, port, payload)
	}
	if len(addr) != 4 || addr[0] != 8 {
		t.Fatalf("bad addr bytes: %v", addr)
	}
}

func TestParseUDPReqHeaderDomain(t *testing.T) {
	b := []byte{0, 0, 0, atypDomain, 3, 'd', 'n', 's', 0, 35, 9, 9}
	atyp, _, host, port, payload, ok := parseUDPReqHeader(b)
	if !ok || atyp != atypDomain || host != "dns" || port != 35 || len(payload) != 2 {
		t.Fatalf("bad parse: ok=%v atyp=%v host=%v port=%v payload=%v", ok, atyp, host, port, payload)
	}
}

func TestParseUDPReqHeaderBad(t *testing.T) {
	if _, _, _, _, _, ok := parseUDPReqHeader([]byte{1, 0, 0, atypIPv4, 1, 2, 3, 4, 0, 53}); ok {
		t.Fatal("rsv nonzero should fail")
	}
	ip6 := []byte{0, 0, 0, atypIPv6}
	ip6 = append(ip6, make([]byte, 16)...)
	ip6 = append(ip6, 0, 53)
	if _, _, _, _, _, ok := parseUDPReqHeader(ip6); ok {
		t.Fatal("ipv6 should fail")
	}
	if _, _, _, _, _, ok := parseUDPReqHeader([]byte{0, 0, 0, 0x09, 1, 2, 3, 4, 0, 53}); ok {
		t.Fatal("unknown atyp should fail")
	}
}

func TestBuildUDPRespHeader(t *testing.T) {
	pkt := buildUDPRespHeader(atypIPv4, []byte{8, 8, 8, 8}, 53, []byte{1, 2})
	if len(pkt) != 12 {
		t.Fatalf("bad packet len: %v", pkt)
	}
	head := pkt[:10]
	want := []byte{0, 0, 0, atypIPv4, 8, 8, 8, 8, 0, 53}
	for i := range want {
		if head[i] != want[i] {
			t.Fatalf("bad header: %v", pkt)
		}
	}
	if pkt[10] != 1 || pkt[11] != 2 {
		t.Fatalf("bad data: %v", pkt)
	}
}

func TestSocksReplyWith(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	go func() {
		_ = socksReplyWith(s, repSuccess, net.IPv4(1, 2, 3, 4), 5555)
		_ = s.Close()
	}()
	b := make([]byte, 10)
	if _, err := c.Read(b); err != nil {
		t.Fatal(err)
	}
	want := []byte{socksVer5, repSuccess, 0x00, atypIPv4, 1, 2, 3, 4, 0x15, 0xb3}
	for i := range want {
		if b[i] != want[i] {
			t.Fatalf("bad reply: %v", b)
		}
	}
}

func TestServeSocksUDPAssociateRelaysPackets(t *testing.T) {
	echo, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := echo.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteToUDP(buf[:n], addr)
		}
	}()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		serveSocks(conn, nil, func(network, _ string) (net.Conn, error) {
			if network != "udp" {
				return nil, errCmdNotSupported
			}
			return net.Dial("udp4", echo.LocalAddr().String())
		})
	}()

	control, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := control.Write([]byte{socksVer5, 1, authNone}); err != nil {
		t.Fatal(err)
	}
	hello := make([]byte, 2)
	if _, err := io.ReadFull(control, hello); err != nil || hello[0] != socksVer5 || hello[1] != authNone {
		t.Fatalf("handshake failed: %v %v", hello, err)
	}
	if _, err := control.Write([]byte{socksVer5, cmdUDPAssociate, 0, atypIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(control, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != socksVer5 || reply[1] != repSuccess {
		t.Fatalf("UDP associate rejected: %v", reply)
	}
	bnd := &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:10]))}
	if !bnd.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("UDP relay advertised %s, want loopback", bnd.IP)
	}
	relay, err := net.DialUDP("udp4", nil, bnd)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	_ = relay.SetDeadline(time.Now().Add(5 * time.Second))

	target := echo.LocalAddr().(*net.UDPAddr)
	payload := []byte("udp-through-socks")
	packet := []byte{0, 0, 0, atypIPv4, 127, 0, 0, 1, byte(target.Port >> 8), byte(target.Port)}
	packet = append(packet, payload...)
	if _, err := relay.Write(packet); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 2048)
	n, err := relay.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, got, ok := parseUDPReqHeader(response[:n])
	if !ok || string(got) != string(payload) {
		t.Fatalf("UDP payload not relayed: ok=%v payload=%q", ok, got)
	}
}
