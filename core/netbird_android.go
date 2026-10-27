//go:build android

package core

import (
	"context"
	"core/netbirdbridge"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/rules"
	nbnet "github.com/netbirdio/netbird/client/net"
)

const netBirdProxy = "FlClash-NetBird-Internal"

var netBirdDNS atomic.Value
var netBirdMu sync.Mutex
var netBird *netbirdbridge.Manager
var netBirdDirectory string

type netBirdInterfaces struct{}

func (netBirdInterfaces) IFaces() (string, error) {
	th := activeTunHandler.Load()
	if th == nil {
		return "", errors.New("VPN is stopped")
	}
	th.mu.RLock()
	defer th.mu.RUnlock()
	if th.callbacks == nil {
		return "", errors.New("VPN callbacks unavailable")
	}
	return th.callbacks.Interfaces(), nil
}

func setNetBirdDirectory(path string) bool {
	netBirdMu.Lock()
	defer netBirdMu.Unlock()
	if !filepath.IsAbs(path) || (netBird != nil && netBirdDirectory != path) {
		return false
	}
	netBirdDirectory = path
	return true
}

func ensureNetBird() (*netbirdbridge.Manager, error) {
	netBirdMu.Lock()
	defer netBirdMu.Unlock()
	if netBird == nil {
		if !isInit.Load() {
			return nil, errNotInitialized
		}
		if netBirdDirectory == "" {
			return nil, errors.New("NetBird private storage is unavailable")
		}
		manager, err := netbirdbridge.New(netBirdDirectory, netBirdInterfaces{})
		if err != nil {
			return nil, err
		}
		netBird = manager
	}
	return netBird, nil
}

func decorateNetBirdConfig(cfg *config.Config) error {
	manager, err := ensureNetBird()
	if err != nil {
		return err
	}
	settings := manager.Config()
	if !settings.Enabled {
		return nil
	}
	server, err := manager.Endpoint()
	if err != nil {
		return err
	}
	if _, exists := cfg.Proxies[netBirdProxy]; exists {
		return errors.New("profile uses the reserved NetBird proxy name")
	}
	socks, err := outbound.NewSocks5(outbound.Socks5Option{Name: netBirdProxy, Server: "127.0.0.1", Port: server.Port(), UserName: server.Username, Password: server.Password, UDP: true})
	if err != nil {
		return err
	}
	cfg.Proxies[netBirdProxy] = adapter.NewProxy(socks)
	rule, err := rules.ParseRule("IP-CIDR", settings.CIDR, netBirdProxy, []string{"no-resolve"}, nil)
	if err != nil {
		return err
	}
	cfg.Rules = append([]C.Rule{rule}, cfg.Rules...)
	return nil
}

func protectNetBird(fd int32) bool {
	th := activeTunHandler.Load()
	if th == nil {
		return false
	}
	th.mu.RLock()
	defer th.mu.RUnlock()
	return th.callbacks != nil && th.callbacks.Protect(int(fd))
}

func startNetBird() {
	nbnet.SetAndroidProtectSocketFn(protectNetBird)
	manager, err := ensureNetBird()
	if err != nil {
		logError("NetBird initialization failed")
		return
	}
	manager.SetRunning(true)
}
func stopNetBird() {
	netBirdMu.Lock()
	m := netBird
	netBirdMu.Unlock()
	if m != nil {
		m.SetRunning(false)
	}
}
func resetNetBirdNetwork() {
	netBirdMu.Lock()
	m := netBird
	netBirdMu.Unlock()
	if m != nil {
		m.NetworkChanged()
	}
}
func closeNetBird() {
	netBirdMu.Lock()
	m := netBird
	netBird = nil
	netBirdMu.Unlock()
	if m != nil {
		m.Close()
	}
}

func netBirdCIDR() string {
	manager, err := ensureNetBird()
	if err != nil {
		return ""
	}
	cfg := manager.Config()
	if !cfg.Enabled {
		return ""
	}
	return cfg.CIDR
}

type netBirdBase interface {
	C.Tunnel
	P.Tunnel
}
type netBirdTunnel struct{ netBirdBase }

var _ P.Tunnel = netBirdTunnel{}

func (t netBirdTunnel) route(metadata *C.Metadata) {
	if cidr := netBirdCIDR(); cidr != "" {
		prefix, err := netip.ParsePrefix(cidr)
		if err == nil && prefix.Contains(metadata.DstIP.Unmap()) {
			metadata.SpecialProxy = netBirdProxy
		}
	}
}
func (t netBirdTunnel) HandleTCPConn(conn net.Conn, metadata *C.Metadata) {
	t.route(metadata)
	t.netBirdBase.HandleTCPConn(conn, metadata)
}
func (t netBirdTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	t.route(metadata)
	t.netBirdBase.HandleUDPPacket(packet, metadata)
}

func init() {
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		servers, _ := netBirdDNS.Load().([]string)
		if len(servers) == 0 {
			return nil, errors.New("physical DNS is unavailable")
		}
		dialer := net.Dialer{Timeout: 5 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
			var inner error
			err := raw.Control(func(fd uintptr) {
				if activeTunHandler.Load() != nil && !protectNetBird(int32(fd)) {
					inner = errors.New("NetBird DNS socket protection refused")
				}
			})
			if err != nil {
				return err
			}
			return inner
		}}
		var last error
		for _, server := range servers {
			conn, err := dialer.DialContext(ctx, network, server)
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}

	registerMethod(getNetBirdStatusMethod, func(_ *MethodCall) (any, error) {
		m, err := ensureNetBird()
		if err != nil {
			return netbirdbridge.Status{}, err
		}
		return m.Status(), nil
	})
	registerMethod(configureNetBirdMethod, withFallible(func(c netbirdbridge.Config) (bool, error) {
		m, err := ensureNetBird()
		if err != nil {
			return false, err
		}
		err = m.Configure(c)
		return err == nil, err
	}))
}
