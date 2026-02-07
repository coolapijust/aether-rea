//go:build !darwin && !linux && !windows

package systemproxy

import "fmt"

func EnableProxy(address string, isHttp bool) error {
	return fmt.Errorf("system proxy not supported on this platform")
}

func DisableProxy() error {
	return fmt.Errorf("system proxy not supported on this platform")
}

func EnableSocksProxy(address string) error {
	return EnableProxy(address, false)
}

func DisableSocksProxy() error {
	return DisableProxy()
}
