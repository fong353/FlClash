package netbirdbridge

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/transport/socks5"
)

type testNetwork struct {
	tcp, udp       string
	dials, packets atomic.Int32
}

func (n *testNetwork) Allowed(address string) bool { return address == "10.10.10.5:12345" }
func (n *testNetwork) Dial(ctx context.Context, address string) (net.Conn, error) {
	n.dials.Add(1)
	return (&net.Dialer{}).DialContext(ctx, "tcp", n.tcp)
}
func (n *testNetwork) ListenUDP() (net.PacketConn, error) {
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return &testPacketConn{c, n.udp, &n.packets}, nil
}

type testPacketConn struct {
	net.PacketConn
	target string
	count  *atomic.Int32
}

func (p *testPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	p.count.Add(1)
	target, err := net.ResolveUDPAddr("udp", p.target)
	if err != nil {
		return 0, err
	}
	return p.PacketConn.WriteTo(b, target)
}
func (p *testPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, _, err := p.PacketConn.ReadFrom(b)
	return n, &net.UDPAddr{IP: net.IPv4(10, 10, 10, 5), Port: 12345}, err
}

func request(t *testing.T, s *Server, password string, command byte, target string) (net.Conn, byte, socks5.Addr) {
	t.Helper()
	c, err := net.Dial("tcp", s.Address())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = c.Write([]byte{5, 1, 2}); err != nil {
		t.Fatal(err)
	}
	var b [2]byte
	if _, err = io.ReadFull(c, b[:]); err != nil {
		t.Fatal(err)
	}
	credentials := append([]byte{1, byte(len(s.Username))}, []byte(s.Username)...)
	credentials = append(credentials, byte(len(password)))
	credentials = append(credentials, []byte(password)...)
	if _, err = c.Write(credentials); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(c, b[:]); err != nil {
		t.Fatal(err)
	}
	if b[1] != 0 {
		return c, 255, nil
	}
	if _, err = c.Write(append([]byte{5, command, 0}, socks5.ParseAddr(target)...)); err != nil {
		t.Fatal(err)
	}
	var head [3]byte
	if _, err = io.ReadFull(c, head[:]); err != nil {
		t.Fatal(err)
	}
	address, err := socks5.ReadAddr0(c)
	if err != nil {
		t.Fatal(err)
	}
	return c, head[1], address
}

func TestSOCKSTCPAndAccessBoundary(t *testing.T) {
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	network := &testNetwork{tcp: echo.Addr().String()}
	server, err := NewServer(network)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_, code, _ := request(t, server, "wrong", socks5.CmdConnect, "10.10.10.5:12345")
	if code != 255 {
		t.Fatal("wrong credentials accepted")
	}
	for _, target := range []string{"1.1.1.1:443", "127.0.0.1:22", "example.com:80"} {
		_, code, _ = request(t, server, server.Password, socks5.CmdConnect, target)
		if code != 2 {
			t.Fatalf("forbidden target %s accepted: %d", target, code)
		}
	}
	if network.dials.Load() != 0 {
		t.Fatal("forbidden traffic reached dialer")
	}
	c, code, _ := request(t, server, server.Password, socks5.CmdConnect, "10.10.10.5:12345")
	if code != 0 {
		t.Fatalf("connect reply %d", code)
	}
	if _, err = c.Write([]byte("through-netbird")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len("through-netbird"))
	if _, err = io.ReadFull(c, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "through-netbird" {
		t.Fatalf("wrong response %q", data)
	}
	server.CloseSessions()
	if _, err = c.Read(data); err == nil {
		t.Fatal("session survived stop")
	}
}

func TestSOCKSUDPAndControlLifetime(t *testing.T) {
	echo, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 1024)
		for {
			n, a, err := echo.ReadFrom(b)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(b[:n], a)
		}
	}()
	network := &testNetwork{udp: echo.LocalAddr().String()}
	server, err := NewServer(network)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	control, code, address := request(t, server, server.Password, socks5.CmdUDPAssociate, "0.0.0.0:0")
	if code != 0 {
		t.Fatalf("associate reply %d", code)
	}
	udp, err := net.DialUDP("udp4", nil, address.UDPAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(3 * time.Second))
	packet, _ := socks5.EncodeUDPPacket(socks5.ParseAddr("1.1.1.1:53"), []byte("blocked"))
	if _, err = udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	packet, _ = socks5.EncodeUDPPacket(socks5.ParseAddr("10.10.10.5:12345"), []byte("udp-overlay"))
	if _, err = udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 1024)
	n, err := udp.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	from, data, err := socks5.DecodeUDPPacket(b[:n])
	if err != nil {
		t.Fatal(err)
	}
	if from.String() != "10.10.10.5:12345" || string(data) != "udp-overlay" {
		t.Fatalf("wrong UDP response %s %q", from, data)
	}
	if network.packets.Load() != 1 {
		t.Fatal("disallowed UDP destination was forwarded")
	}
	control.Close()
	server.CloseSessions()
}

func TestSOCKSRejectsUnauthenticatedClient(t *testing.T) {
	s, err := NewServer(&testNetwork{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("tcp", s.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	_, _ = c.Write([]byte{5, 1, 0})
	var response [2]byte
	if _, err = io.ReadFull(c, response[:]); err != nil {
		t.Fatal(err)
	}
	if response != [2]byte{5, 255} {
		t.Fatalf("no-auth was accepted: %v", response)
	}
}
