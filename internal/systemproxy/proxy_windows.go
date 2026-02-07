//go:build windows

package systemproxy

import (
	"fmt"
	"os/exec"
	"syscall"
)

var (
	modwininet            = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOption = modwininet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

func notifyProxyChange() {
	// InternetSetOption(NULL, INTERNET_OPTION_SETTINGS_CHANGED, NULL, 0)
	// InternetSetOption(NULL, INTERNET_OPTION_REFRESH, NULL, 0)
	procInternetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	procInternetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
}

func EnableSocksProxy(address string) error {
	host, port, err := NormalizeAddress(address)
	if err != nil {
		return err
	}
	proxyServer := fmt.Sprintf("socks5=%s:%s", host, port)
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run(); err != nil {
		return fmt.Errorf("enable proxy registry: %w", err)
	}
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyServer, "/f").Run(); err != nil {
		return fmt.Errorf("set proxy server: %w", err)
	}
	notifyProxyChange()
	return nil
}

func DisableSocksProxy() error {
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run(); err != nil {
		return fmt.Errorf("disable proxy registry: %w", err)
	}
	notifyProxyChange()
	return nil
}
