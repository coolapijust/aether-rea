//go:build windows

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
	proxyServer := fmt.Sprintf("socks=%s:%s", host, port)
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run(); err != nil {
		return fmt.Errorf("enable proxy registry: %w", err)
	}
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyServer, "/f").Run(); err != nil {
		return fmt.Errorf("set proxy server: %w", err)
	}
	return nil
}

func DisableSocksProxy() error {
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run(); err != nil {
		return fmt.Errorf("disable proxy registry: %w", err)
	}
	return nil
}
