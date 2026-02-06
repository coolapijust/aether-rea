//go:build !darwin && !linux && !windows

package systemproxy

import "fmt"

func EnableSocksProxy(address string) error {
	return fmt.Errorf("system proxy not supported on this platform")
}

func DisableSocksProxy() error {
	return fmt.Errorf("system proxy not supported on this platform")
}
