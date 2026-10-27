//go:build !android

package core

import (
	"errors"
	"github.com/metacubex/mihomo/config"
)

func decorateNetBirdConfig(*config.Config) error { return nil }
func resetNetBirdNetwork()                       {}
func closeNetBird()                              {}

func init() {
	unsupported := func(_ *MethodCall) (any, error) {
		return nil, errors.New("NetBird integration is available on Android only")
	}
	registerMethod(getNetBirdStatusMethod, unsupported)
	registerMethod(configureNetBirdMethod, unsupported)
}
