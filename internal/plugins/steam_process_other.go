//go:build !linux

package plugins

import (
	"context"
	"errors"
)

func SteamRunning() bool {
	return false
}

func CloseSteam(context.Context) error {
	return errors.New("closing Steam automatically is supported only on Linux")
}
