package main

import (
	"fmt"
	"log"
	"os"
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
	procInternetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
	procInternetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
}

func EnableSocksProxy(address string) error {
	proxyServer := fmt.Sprintf("socks=%s", address) // Try 'socks=' instead of 'socks5='
	fmt.Printf("Setting ProxyServer to: %s\n", proxyServer)

	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run(); err != nil {
		return fmt.Errorf("enable proxy registry: %w", err)
	}
	if err := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxyServer, "/f").Run(); err != nil {
		return fmt.Errorf("set proxy server: %w", err)
	}
	notifyProxyChange()
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: proxy-test <address:port>")
		return
	}
	addr := os.Args[1]
	if err := EnableSocksProxy(addr); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Proxy enabled successfully.")
}
