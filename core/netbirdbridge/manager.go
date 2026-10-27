package netbirdbridge

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	nb "github.com/netbirdio/netbird/client/embed"
	"github.com/netbirdio/netbird/client/netevents"
)

type Peer struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Connected bool   `json:"connected"`
	Relayed   bool   `json:"relayed"`
}

type Status struct {
	Config
	Enrolled   bool   `json:"enrolled"`
	State      string `json:"state"`
	IP         string `json:"ip"`
	Management bool   `json:"management"`
	Signal     bool   `json:"signal"`
	Peers      []Peer `json:"peers"`
	Error      string `json:"error"`
}

type Manager struct {
	op         sync.Mutex
	mu         sync.RWMutex
	dir        string
	config     Config
	server     *Server
	client     *nb.Client
	events     *netevents.Manager
	cancel     context.CancelFunc
	done       chan struct{}
	state      string
	lastError  string
	discover   nb.InterfaceDiscover
	desired    bool
	wake       chan struct{}
	closing    chan struct{}
	workerDone chan struct{}
	closeOnce  sync.Once
}

func New(dir string, discover nb.InterfaceDiscover) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("NetBird directory must be private and must not be a symlink")
	}
	m := &Manager{dir: dir, config: DefaultConfig(), state: "stopped", discover: discover}
	if err := readPrivate(m.settingsPath(), &m.config); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := m.config.Validate(); err != nil {
		return nil, err
	}
	m.wake = make(chan struct{}, 1)
	m.closing = make(chan struct{})
	m.workerDone = make(chan struct{})
	go m.run()
	return m, nil
}

func (m *Manager) settingsPath() string { return filepath.Join(m.dir, "settings.json") }
func (m *Manager) identityPath() string { return filepath.Join(m.dir, "identity.json") }

func (m *Manager) Configure(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired || m.cancel != nil {
		return errors.New("stop the VPN before changing NetBird settings")
	}
	if m.enrolled() && (c.ManagementURL != m.config.ManagementURL || c.DeviceName != m.config.DeviceName) {
		return errors.New("this identity belongs to its original management server and device name")
	}
	if c.SetupKey == "" {
		c.SetupKey = m.config.SetupKey
	}
	if m.enrolled() {
		c.SetupKey = ""
	}
	if c.Enabled && !m.enrolled() && c.SetupKey == "" {
		return errors.New("first connection requires a one-time NetBird setup key")
	}
	if err := writePrivate(m.settingsPath(), c); err != nil {
		return err
	}
	m.config = c
	return nil
}

func (m *Manager) enrolled() bool {
	var identity struct{ PrivateKey string }
	return readPrivate(m.identityPath(), &identity) == nil && identity.PrivateKey != "" && fileExists(filepath.Join(m.dir, "enrolled"))
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.config
	c.SetupKey = ""
	return c
}

func (m *Manager) Endpoint() (*Server, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if m.server == nil {
		s, err := NewServer(m)
		if err != nil {
			return nil, err
		}
		m.server = s
	}
	return m.server, nil
}

func (m *Manager) start() {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.desired || !m.config.Enabled || m.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.state = "connecting"
	m.lastError = ""
	go m.connect(ctx, m.config, m.done)
}

func (m *Manager) connect(ctx context.Context, cfg Config, done chan struct{}) {
	defer close(done)
	var identity struct{ PrivateKey string }
	var privateKey, setupKey string
	if m.enrolled() {
		if err := readPrivate(m.identityPath(), &identity); err != nil {
			m.failed("cannot read private NetBird identity")
			return
		}
		privateKey = identity.PrivateKey
	} else {
		setupKey = cfg.SetupKey
		if !fileExists(m.identityPath()) {
			if err := writePrivate(m.identityPath(), map[string]any{"DisableDNS": true, "ServerSSHAllowed": false, "RemoteJobsAllowed": false, "DisableNotifications": true}); err != nil {
				m.failed("cannot create private NetBird identity")
				return
			}
		}
	}
	events := netevents.NewManager(nil)
	port := 0
	client, err := nb.New(nb.Options{
		DeviceName: cfg.DeviceName, ManagementURL: cfg.ManagementURL, SetupKey: setupKey, PrivateKey: privateKey,
		ConfigPath:          m.identityPath(),
		DisableClientRoutes: true, DisableIPv6: true, BlockInbound: true, BlockLANAccess: true,
		WireguardPort: &port, InterfaceDiscover: m.discover, NetEvents: events,
	})
	if err != nil {
		m.failed("cannot initialize embedded NetBird")
		return
	}
	startCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	err = client.Start(startCtx)
	cancel()
	if err != nil {
		m.failed("NetBird connection failed; check server, setup key and network")
		return
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = client.Stop(stopCtx)
	}()
	m.mu.Lock()
	if ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	if err := os.WriteFile(filepath.Join(m.dir, "enrolled"), []byte("enrolled\n"), 0600); err != nil {
		m.mu.Unlock()
		m.failed("cannot persist NetBird enrollment")
		return
	}
	m.config.SetupKey = ""
	if err := writePrivate(m.settingsPath(), m.config); err != nil {
		m.mu.Unlock()
		m.failed("cannot clear consumed setup key")
		return
	}
	m.client = client
	m.events = events
	m.state = "connected"
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	m.client = nil
	m.events = nil
	m.mu.Unlock()
}

func (m *Manager) failed(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = "error"
	m.lastError = message
}

func (m *Manager) stop() {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.client = nil
	m.events = nil
	m.mu.Unlock()
	if m.server != nil {
		m.server.CloseSessions()
	}
	if cancel != nil {
		cancel()
		<-done
	}
	m.mu.Lock()
	m.cancel = nil
	m.done = nil
	m.state = "stopped"
	m.mu.Unlock()
}

func (m *Manager) SetRunning(running bool) {
	m.mu.Lock()
	select {
	case <-m.closing:
		m.mu.Unlock()
		return
	default:
	}
	m.desired = running
	if !running && m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) run() {
	defer close(m.workerDone)
	for {
		select {
		case <-m.closing:
			m.stop()
			return
		case <-m.wake:
			m.stop()
			m.mu.RLock()
			running := m.desired
			m.mu.RUnlock()
			if running {
				m.start()
			}
		}
	}
}

func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.desired = false
		if m.cancel != nil {
			m.cancel()
		}
		close(m.closing)
		m.mu.Unlock()
		<-m.workerDone
		m.op.Lock()
		defer m.op.Unlock()
		if m.server != nil {
			m.server.Close()
			m.server = nil
		}
	})
}

func (m *Manager) NetworkChanged() {
	m.mu.RLock()
	e := m.events
	m.mu.RUnlock()
	if e != nil {
		e.NotifyNetworkChange()
	}
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	cfg, state, message, c := m.config, m.state, m.lastError, m.client
	m.mu.RUnlock()
	cfg.SetupKey = ""
	s := Status{Config: cfg, Enrolled: m.enrolled(), State: state, Error: message, Peers: []Peer{}}
	if c == nil {
		return s
	}
	status, err := c.Status()
	if err != nil {
		return s
	}
	s.IP = status.LocalPeerState.IP
	s.Management = status.ManagementState.Connected
	s.Signal = status.SignalState.Connected
	for _, p := range status.Peers {
		s.Peers = append(s.Peers, Peer{Name: p.FQDN, IP: p.IP, Connected: p.ConnStatus == nb.PeerStatusConnected, Relayed: p.Relayed})
	}
	return s
}

func (m *Manager) Allowed(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	prefix, err := netip.ParsePrefix(cfg.CIDR)
	return err == nil && cfg.Enabled && prefix.Contains(ip.Unmap())
}

func (m *Manager) Dial(ctx context.Context, address string) (net.Conn, error) {
	if !m.Allowed(address) {
		return nil, errors.New("target is outside the NetBird network")
	}
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("NetBird is not connected")
	}
	return client.Dial(ctx, "tcp", address)
}

func (m *Manager) ListenUDP() (net.PacketConn, error) {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return nil, errors.New("NetBird is not connected")
	}
	return client.ListenUDP(":0")
}
