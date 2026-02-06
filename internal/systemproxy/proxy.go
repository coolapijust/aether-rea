package systemproxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// NormalizeAddress returns a host and port string for a proxy address.
func NormalizeAddress(address string) (string, string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", fmt.Errorf("proxy address is empty")
	}

	host, port, err := net.SplitHostPort(address)
	if err == nil {
		return host, port, nil
	}

	if !strings.Contains(address, ":") {
		return "", "", fmt.Errorf("proxy address must include port")
	}

	parts := strings.Split(address, ":")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("proxy address must include port")
	}

	host = strings.Join(parts[:len(parts)-1], ":")
	port = parts[len(parts)-1]
	if host == "" || port == "" {
		return "", "", fmt.Errorf("proxy address must include host and port")
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid port: %s", port)
	}
	return host, port, nil
}
