//go:build darwin

package systemproxy

import (
	"fmt"
	"os/exec"
	"strings"
)

func EnableSocksProxy(address string) error {
	host, port, err := NormalizeAddress(address)
	if err != nil {
		return err
	}
	services, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := exec.Command("networksetup", "-setsocksfirewallproxy", service, host, port).Run(); err != nil {
			return fmt.Errorf("set proxy for %s: %w", service, err)
		}
		if err := exec.Command("networksetup", "-setsocksfirewallproxystate", service, "on").Run(); err != nil {
			return fmt.Errorf("enable proxy for %s: %w", service, err)
		}
	}
	return nil
}

func DisableSocksProxy() error {
	services, err := listNetworkServices()
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := exec.Command("networksetup", "-setsocksfirewallproxystate", service, "off").Run(); err != nil {
			return fmt.Errorf("disable proxy for %s: %w", service, err)
		}
	}
	return nil
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
