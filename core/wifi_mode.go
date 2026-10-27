package core

import "github.com/metacubex/mihomo/tunnel"

var homeWifiMode string // guarded by configMu

func effectiveMode(requested tunnel.TunnelMode) tunnel.TunnelMode {
	if homeWifiMode == "direct" {
		return tunnel.Direct
	}
	if homeWifiMode == "rule" {
		return tunnel.Rule
	}
	return requested
}

func setHomeWifiMode(mode string) {
	if mode != "" && mode != "direct" && mode != "rule" {
		return
	}
	configMu.Lock()
	defer configMu.Unlock()
	homeWifiMode = mode
	if currentConfig == nil {
		return
	}
	next := effectiveMode(tunnel.Rule)
	if currentConfig.General.Mode != next {
		currentConfig.General.Mode = next
		tunnel.SetMode(next)
		changeRoute(routeMoved)
	}
}
