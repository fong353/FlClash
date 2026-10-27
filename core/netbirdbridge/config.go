package netbirdbridge

import (
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Enabled       bool   `json:"enabled"`
	ManagementURL string `json:"managementUrl"`
	DeviceName    string `json:"deviceName"`
	CIDR          string `json:"cidr"`
	SetupKey      string `json:"setupKey,omitempty"`
}

func DefaultConfig() Config {
	return Config{ManagementURL: "https://netbird.example.com", DeviceName: "flclash-android", CIDR: "10.10.10.0/24"}
}

func (c Config) Validate() error {
	u, err := url.Parse(c.ManagementURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("NetBird requires an HTTPS management URL without credentials, path, query or fragment")
	}
	if len(c.DeviceName) == 0 || len(c.DeviceName) > 63 || strings.ContainsAny(c.DeviceName, "\r\n\t /") {
		return errors.New("invalid NetBird device name")
	}
	p, err := netip.ParsePrefix(c.CIDR)
	if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() < 8 || p != p.Masked() {
		return errors.New("NetBird network must be a canonical private IPv4 CIDR")
	}
	if len(c.SetupKey) > 256 || strings.ContainsAny(c.SetupKey, "\r\n\t ") {
		return errors.New("invalid NetBird setup key")
	}
	return nil
}

func readPrivate(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("NetBird state must be a private regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func writePrivate(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".netbird-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}
