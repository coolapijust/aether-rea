//go:build linux

package systemproxy

import (
	"fmt"
	"os/exec"
)

// EnableProxy enables the system proxy.
func EnableProxy(address string, isHttp bool) error {
	host, port, err := NormalizeAddress(address)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("gsettings"); err != nil {
		return fmt.Errorf("gsettings not available: %w", err)
	}
	if err := exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "manual").Run(); err != nil {
		return fmt.Errorf("enable proxy mode: %w", err)
	}
	if isHttp {
		// HTTP
		exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "host", host).Run()
		exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "port", port).Run()
		exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "enabled", "true").Run()
		// HTTPS
		exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "host", host).Run()
		exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "port", port).Run()
	} else {
		if err := exec.Command("gsettings", "set", "org.gnome.system.proxy.socks", "host", host).Run(); err != nil {
			return fmt.Errorf("set socks host: %w", err)
		}
		if err := exec.Command("gsettings", "set", "org.gnome.system.proxy.socks", "port", port).Run(); err != nil {
			return fmt.Errorf("set socks port: %w", err)
		}
	}
	return nil
}

// DisableProxy disables the system proxy.
func DisableProxy() error {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return fmt.Errorf("gsettings not available: %w", err)
	}
	if err := exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none").Run(); err != nil {
		return fmt.Errorf("disable proxy mode: %w", err)
	}
	return nil
}

func EnableSocksProxy(address string) error {
	return EnableProxy(address, false)
}

func DisableSocksProxy() error {
	return DisableProxy()
}
