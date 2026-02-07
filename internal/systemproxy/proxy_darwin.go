//go:build darwin

package systemproxy

import (
	"fmt"
	"os/exec"
	"strings"
)

// EnableProxy enables the system proxy.
func EnableProxy(address string, isHttp bool) error {
	host, port, err := NormalizeAddress(address)
	if err != nil {
		return err
	}
	services, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if isHttp {
			// Set HTTP proxy
			if err := exec.Command("networksetup", "-setwebproxy", service, host, port).Run(); err != nil {
				return fmt.Errorf("set http proxy for %s: %w", service, err)
			}
			if err := exec.Command("networksetup", "-setwebproxystate", service, "on").Run(); err != nil {
				return fmt.Errorf("enable http proxy for %s: %w", service, err)
			}
			// Set HTTPS proxy
			if err := exec.Command("networksetup", "-setsecurewebproxy", service, host, port).Run(); err != nil {
				return fmt.Errorf("set https proxy for %s: %w", service, err)
			}
			if err := exec.Command("networksetup", "-setsecurewebproxystate", service, "on").Run(); err != nil {
				return fmt.Errorf("enable https proxy for %s: %w", service, err)
			}
		} else {
			// Set SOCKS proxy
			if err := exec.Command("networksetup", "-setsocksfirewallproxy", service, host, port).Run(); err != nil {
				return fmt.Errorf("set socks proxy for %s: %w", service, err)
			}
			if err := exec.Command("networksetup", "-setsocksfirewallproxystate", service, "on").Run(); err != nil {
				return fmt.Errorf("enable socks proxy for %s: %w", service, err)
			}
		}
	}
	return nil
}

// DisableProxy disables the system proxy.
func DisableProxy() error {
	services, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		exec.Command("networksetup", "-setwebproxystate", service, "off").Run()
		exec.Command("networksetup", "-setsecurewebproxystate", service, "off").Run()
		exec.Command("networksetup", "-setsocksfirewallproxystate", service, "off").Run()
	}
	return nil
}

func EnableSocksProxy(address string) error {
	return EnableProxy(address, false)
}

func DisableSocksProxy() error {
	return DisableProxy()
}

func listNetworkServices() ([]string, error) {
	output, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, fmt.Errorf("list network services: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	services := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "An asterisk") {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no active network services found")
	}
	return services, nil
}
