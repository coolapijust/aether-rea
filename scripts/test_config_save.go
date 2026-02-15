package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	config := map[string]interface{}{
		"server_addr": "example.com",
		"server_port": 443,
		"server_path": "/v5",
		"psk":         "test-psk-from-script",
		"listen_addr": "127.0.0.1:1080",
		"http_proxy_addr": "127.0.0.1:1081",
		"rotation": map[string]interface{}{
			"enabled": true,
			"min_interval_ms": 300000,
			"max_interval_ms": 600000,
			"pre_warm_ms": 10000,
		},
		"bypass_cn": true,
		"block_ads": true,
	}

	data, _ := json.Marshal(config)
	resp, err := http.Post("http://127.0.0.1:9880/api/v1/config", "application/json", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("Error calling API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Body: %s\n", string(body))
}
