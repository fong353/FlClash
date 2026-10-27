package netbirdbridge

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStopCancelsPendingEnrollmentWithoutBlockingService(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		buffer := make([]byte, 4096)
		for {
			if _, err := conn.Read(buffer); err != nil {
				return
			}
		}
	}()
	manager, err := New(filepath.Join(t.TempDir(), "state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	settings := DefaultConfig()
	settings.Enabled = true
	settings.SetupKey = "unused-test-key"
	settings.ManagementURL = "https://" + listener.Addr().String()
	if err := manager.Configure(settings); err != nil {
		t.Fatal(err)
	}
	manager.SetRunning(true)
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("enrollment did not reach test listener")
	}
	before := time.Now()
	manager.SetRunning(false)
	if time.Since(before) > 200*time.Millisecond {
		t.Fatal("service stop blocked on NetBird shutdown")
	}
	closed := make(chan struct{})
	go func() { manager.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("pending enrollment ignored cancellation")
	}
}

func TestSettingsPersistenceAndSecretRedaction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "netbird")
	m, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	cfg := DefaultConfig()
	cfg.Enabled = true
	if err = m.Configure(cfg); err == nil {
		t.Fatal("first enrollment accepted without key")
	}
	cfg.SetupKey = "test-one-time-key"
	if err = m.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cfg.SetupKey) {
		t.Fatal("setup key leaked through status")
	}
	info, err := os.Stat(m.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("settings permissions are not private")
	}
	reloaded, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if !reloaded.Config().Enabled {
		t.Fatal("settings were not persisted")
	}
	if err = writePrivate(m.identityPath(), map[string]string{"PrivateKey": "test-private-identity"}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "enrolled"), []byte("enrolled"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.ManagementURL = "https://other.example.com"
	if err = m.Configure(cfg); err == nil {
		t.Fatal("enrolled identity silently moved to another server")
	}
	cfg.ManagementURL = DefaultConfig().ManagementURL
	if err = m.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(m.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "test-one-time-key") {
		t.Fatal("consumed key persisted")
	}
}

func TestConfigurationBoundary(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/?token=x", "https://example.com/path"} {
		c := DefaultConfig()
		c.ManagementURL = url
		if c.Validate() == nil {
			t.Fatalf("unsafe URL accepted: %s", url)
		}
	}
	for _, cidr := range []string{"0.0.0.0/0", "1.1.1.0/24", "127.0.0.0/8", "10.10.10.1/24", "::/0", "10.0.0.0/7"} {
		c := DefaultConfig()
		c.CIDR = cidr
		if c.Validate() == nil {
			t.Fatalf("unsafe CIDR accepted: %s", cidr)
		}
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(link, nil); err == nil {
		t.Fatal("symlink state directory accepted")
	}
	m, err := New(filepath.Join(t.TempDir(), "state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.SetupKey = "test"
	if err = m.Configure(c); err != nil {
		t.Fatal(err)
	}
	if !m.Allowed("[::ffff:10.10.10.5]:22") || m.Allowed("example.com:22") || m.Allowed("10.10.11.5:22") {
		t.Fatal("overlay target validation failed")
	}
}
