package core

import (
	"testing"

	"github.com/metacubex/mihomo/config"

	"github.com/metacubex/mihomo/tunnel"
)

func TestHomeWifiOverridesLiveModeUpdates(t *testing.T) {
	cfg, err := config.ParseRawConfig(config.DefaultRawConfig())
	if err != nil {
		t.Fatal(err)
	}
	withCurrentConfig(t, cfg)
	oldMode := tunnel.Mode()
	t.Cleanup(func() { tunnel.SetMode(oldMode) })
	previous := homeWifiMode
	t.Cleanup(func() { homeWifiMode = previous })
	for _, entry := range []struct {
		policy string
		want   tunnel.TunnelMode
	}{{"direct", tunnel.Direct}, {"rule", tunnel.Rule}} {
		setHomeWifiMode(entry.policy)
		requested := tunnel.Global
		if err := updateConfig(&UpdateParams{Mode: &requested}); err != nil {
			t.Fatal(err)
		}
		if currentConfig.General.Mode != entry.want || tunnel.Mode() != entry.want {
			t.Fatalf("policy %s did not survive a manual/config mode update", entry.policy)
		}
		if effectiveMode(tunnel.Global) != entry.want {
			t.Fatal("full config apply would bypass the policy")
		}
	}
	setHomeWifiMode("")
	if currentConfig.General.Mode != tunnel.Rule {
		t.Fatal("disabling automation must leave a safe rule mode")
	}
	manual := tunnel.Global
	if err := updateConfig(&UpdateParams{Mode: &manual}); err != nil {
		t.Fatal(err)
	}
	if currentConfig.General.Mode != manual {
		t.Fatal("disabling automation must restore manual control")
	}
}
