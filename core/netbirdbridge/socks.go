package netbirdbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/metacubex/mihomo/transport/socks5"
)

type Network interface {
	Allowed(string) bool
	Dial(context.Context, string) (net.Conn, error)
	ListenUDP() (net.PacketConn, error)
}

type Server struct {
	network  Network
	listener net.Listener
	Username string
	Password string
	mu       sync.Mutex
	sessions map[net.Conn]context.CancelFunc
	closed   bool
	slots    chan struct{}
	wg       sync.WaitGroup
}

func NewServer(network Network) (*Server, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{network: network, listener: listener, Username: "netbird", Password: hex.EncodeToString(token), sessions: map[net.Conn]context.CancelFunc{}, slots: make(chan struct{}, 64)}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) Port() int       { return s.listener.Addr().(*net.TCPAddr).Port }
func (s *Server) Address() string { return s.listener.Addr().String() }
func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			cancel()
			conn.Close()
			<-s.slots
			return
		}
		s.sessions[conn] = cancel
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer func() {
				cancel()
				conn.Close()
				s.mu.Lock()
				delete(s.sessions, conn)
				s.mu.Unlock()
				<-s.slots
			}()
			s.serve(ctx, conn)
		}()
	}
}

func (s *Server) CloseSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn, cancel := range s.sessions {
		cancel()
		conn.Close()
	}
}
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.listener.Close()
	s.CloseSessions()
	s.wg.Wait()
}

func (s *Server) authenticate(c net.Conn) error {
	var header [2]byte
	if _, err := io.ReadFull(c, header[:]); err != nil {
		return err
	}
	if header[0] != 5 || header[1] == 0 {
		return errors.New("invalid SOCKS greeting")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	supported := false
	for _, method := range methods {
		if method == 2 {
			supported = true
		}
	}
	if !supported {
		_, _ = c.Write([]byte{5, 255})
		return errors.New("authentication required")
	}
	if _, err := c.Write([]byte{5, 2}); err != nil {
		return err
	}
	if _, err := io.ReadFull(c, header[:]); err != nil {
		return err
	}
	if header[0] != 1 || header[1] == 0 {
		return errors.New("invalid SOCKS authentication")
	}
	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(c, username); err != nil {
		return err
	}
	if _, err := io.ReadFull(c, header[:1]); err != nil {
		return err
	}
	password := make([]byte, int(header[0]))
	if _, err := io.ReadFull(c, password); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(username, []byte(s.Username)) != 1 || subtle.ConstantTimeCompare(password, []byte(s.Password)) != 1 {
		_, _ = c.Write([]byte{1, 1})
		return errors.New("authentication rejected")
	}
	_, err := c.Write([]byte{1, 0})
	return err
}

func reply(c net.Conn, code byte, address string) error {
	addr := socks5.ParseAddr(address)
	if addr == nil {
		return errors.New("invalid reply address")
	}
	_, err := c.Write(append([]byte{5, code, 0}, addr...))
	return err
}

func (s *Server) serve(ctx context.Context, c net.Conn) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if err := s.authenticate(c); err != nil {
		return
	}
	var header [3]byte
	if _, err := io.ReadFull(c, header[:]); err != nil {
		return
	}
	if header[0] != 5 || header[2] != 0 {
		return
	}
	address, err := socks5.ReadAddr0(c)
	if err != nil {
		return
	}
	switch header[1] {
	case socks5.CmdConnect:
		if !s.network.Allowed(address.String()) {
			_ = reply(c, 2, "127.0.0.1:0")
			return
		}
		dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		remote, err := s.network.Dial(dialCtx, address.String())
		cancel()
		if err != nil {
			_ = reply(c, 4, "127.0.0.1:0")
			return
		}
		defer remote.Close()
		if err := reply(c, 0, "127.0.0.1:0"); err != nil {
			return
		}
		c.SetDeadline(time.Time{})
		stop := context.AfterFunc(ctx, func() { remote.Close() })
		defer stop()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = io.Copy(remote, c)
			if writer, ok := remote.(interface{ CloseWrite() error }); ok {
				_ = writer.CloseWrite()
			} else {
				_ = remote.Close()
			}
		}()
		_, _ = io.Copy(c, remote)
		c.Close()
		remote.Close()
		<-done
	case socks5.CmdUDPAssociate:
		s.serveUDP(ctx, c, address)
	default:
		_ = reply(c, 7, "127.0.0.1:0")
	}
}

func (s *Server) serveUDP(ctx context.Context, c net.Conn, requested socks5.Addr) {
	requestAddr := requested.UDPAddr()
	if requestAddr == nil || (!requestAddr.IP.IsUnspecified() && !requestAddr.IP.IsLoopback()) {
		_ = reply(c, 2, "127.0.0.1:0")
		return
	}
	local, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return
	}
	defer local.Close()
	remote, err := s.network.ListenUDP()
	if err != nil {
		_ = reply(c, 4, "127.0.0.1:0")
		return
	}
	defer remote.Close()
	if err := reply(c, 0, local.LocalAddr().String()); err != nil {
		return
	}
	c.SetDeadline(time.Time{})
	udpCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cleanup := context.AfterFunc(udpCtx, func() { local.Close(); remote.Close(); c.Close() })
	defer cleanup()
	var mu sync.Mutex
	var sender *net.UDPAddr
	destinations := map[string]bool{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 65535)
		for {
			local.SetReadDeadline(time.Now().Add(2 * time.Minute))
			n, source, err := local.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !source.IP.IsLoopback() || (requestAddr.Port != 0 && source.Port != requestAddr.Port) {
				continue
			}
			addr, payload, err := socks5.DecodeUDPPacket(buf[:n])
			if err != nil || !s.network.Allowed(addr.String()) {
				continue
			}
			mu.Lock()
			if sender == nil {
				sender = source
			}
			matches := sender.String() == source.String()
			if matches && len(destinations) < 256 {
				destinations[addr.String()] = true
			}
			permitted := matches && destinations[addr.String()]
			mu.Unlock()
			if !permitted {
				continue
			}
			target := addr.UDPAddr()
			if target == nil {
				continue
			}
			if _, err := remote.WriteTo(payload, target); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 65535)
		for {
			n, source, err := remote.ReadFrom(buf)
			if err != nil {
				return
			}
			if !s.network.Allowed(source.String()) {
				continue
			}
			mu.Lock()
			target, allowed := sender, destinations[source.String()]
			mu.Unlock()
			if target == nil || !allowed {
				continue
			}
			packet, err := socks5.EncodeUDPPacket(socks5.ParseAddr(source.String()), buf[:n])
			if err != nil {
				continue
			}
			if _, err := local.WriteToUDP(packet, target); err != nil {
				return
			}
		}
	}()
	_, _ = io.Copy(io.Discard, c)
	cancel()
	local.Close()
	remote.Close()
	wg.Wait()
}
