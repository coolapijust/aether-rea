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

// EnableProxy enables the system proxy.
func EnableProxy(address string, isHttp bool) error {
	host, port, err := NormalizeAddress(address)
	if err != nil {
		return err
	}
	
	proxyServer := ""
	if isHttp {
		// For Windows 11, setting http and https separately is more reliable
		proxyServer = fmt.Sprintf("http=%s:%s;https=%s:%s", host, port, host, port)
	} else {
		proxyServer = fmt.Sprintf("socks=%s:%s", host, port)
	}
	
	return setProxy(proxyServer, true)
}

// DisableProxy disables the system proxy.
func DisableProxy() error {
	return setProxy("", false)
}

// setProxy handles the actual registry modification
func setProxy(proxyServer string, enable bool) error {
	enableVal := "0"
	if enable {
		enableVal = "1"
	}

	// Set ProxyEnable
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", enableVal, "/f").Run(); err != nil {
		return fmt.Errorf("enable proxy registry: %w", err)
	}
	
	// Set ProxyServer if enabling
	if enable {
		if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyServer, "/f").Run(); err != nil {
			return fmt.Errorf("set proxy server: %w", err)
		}
	}
	
	notifyProxyChange()
	return nil
}

// Legacy wrappers for backward compatibility if needed
func EnableSocksProxy(address string) error {
	return EnableProxy(address, false)
}

func DisableSocksProxy() error {
	return DisableProxy()
}
