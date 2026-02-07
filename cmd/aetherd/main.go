// aetherd - Aether-Realist Core Daemon
// Provides SOCKS5 proxy with WebTransport backend and HTTP API for GUI
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"aether-rea/internal/api"
	"aether-rea/internal/core"
	"aether-rea/internal/util"
)

func main() {
	// Single instance protection
	lock, err := util.AcquireLock("aetherd")
	if err != nil {
		log.Fatalf("Fatal: %v", err)
	}
	defer lock.Release()
	var (
		listenAddr = flag.String("listen", "127.0.0.1:1080", "SOCKS5 listen address")
		httpAddr   = flag.String("http", "", "HTTP proxy listen address (e.g. 127.0.0.1:1081)")
		apiAddr    = flag.String("api", "127.0.0.1:9880", "HTTP API listen address")
		url        = flag.String("url", "", "WebTransport endpoint URL")
		psk        = flag.String("psk", "", "Pre-shared key")
	)
	flag.Parse()

	// Create Core
	c := core.New()

	// Redirect logs to both stdout and Core event stream
	log.SetOutput(io.MultiWriter(os.Stdout, c.GetLogWriter()))

	log.Println("Starting Aether-Realist Daemon...")

	// Prepare config
	config := core.SessionConfig{
		ListenAddr:    *listenAddr,
		HttpProxyAddr: *httpAddr,
		URL:           *url,
		PSK:           *psk,
	}

	// Load persisted config
	cm, err := core.NewConfigManager()
	if err == nil {
		if loaded, err := cm.Load(); err == nil && loaded != nil {
			log.Printf("Loaded persisted config")
			// Merge with flags (flags take precedence if set, but here we just use loaded)
			// For simplicity, if loaded exists, use it as base
			if *url == "" && loaded.URL != "" {
				config.URL = loaded.URL
			}
			if *psk == "" && loaded.PSK != "" {
				config.PSK = loaded.PSK
			}
			
			if loaded.ListenAddr != "" && *listenAddr == "127.0.0.1:1080" {
				config.ListenAddr = loaded.ListenAddr
			}
			
			if *httpAddr == "" && loaded.HttpProxyAddr != "" {
				config.HttpProxyAddr = loaded.HttpProxyAddr
			}
		}
	}

	// Start Core with config
	if err := c.Start(config); err != nil {
		log.Printf("Failed to start core: %v", err)
		return
	}

	// Start HTTP API server
	server := api.NewServer(c, *apiAddr)
	if err := server.Start(); err != nil {
		log.Printf("Failed to start API server: %v", err)
		return
	}

	log.Printf("HTTP API listening on %s", server.Addr())

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")

	// Graceful shutdown
	if err := server.Stop(); err != nil {
		log.Printf("Error stopping server: %v", err)
	}

	if err := c.Close(); err != nil {
		log.Printf("Error closing core: %v", err)
	}

	log.Println("Goodbye")
}
