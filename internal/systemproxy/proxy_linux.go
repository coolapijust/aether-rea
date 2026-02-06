//go:build linux

package systemproxy

import (
	"fmt"
	"os/exec"
)

func EnableSocksProxy(address string) error {
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
	if err := exec.Command("gsettings", "set", "org.gnome.system.proxy.socks", "host", host).Run(); err != nil {
		return fmt.Errorf("set socks host: %w", err)
	}
	if err := exec.Command("gsettings", "set", "org.gnome.system.proxy.socks", "port", port).Run(); err != nil {
		return fmt.Errorf("set socks port: %w", err)
	}
	return nil
}

func DisableSocksProxy() error {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return fmt.Errorf("gsettings not available: %w", err)
	}
	if err := exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none").Run(); err != nil {
		return fmt.Errorf("disable proxy mode: %w", err)
	}
	return nil
}
