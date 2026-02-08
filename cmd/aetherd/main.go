// aetherd - Aether-Realist Core Daemon
// Provides SOCKS5 proxy with WebTransport backend and HTTP API for GUI
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aether-rea/internal/api"
	"aether-rea/internal/core"
	"aether-rea/internal/systemproxy"
	"aether-rea/internal/util"
)

func main() {
	// Add panic recovery to catch hidden crashes
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL PANIC RECOVERED: %v", r)
			// Small sleep to ensure log is visible in console if it flash-opens
			time.Sleep(2 * time.Second)
			os.Exit(2)
		}
	}()

	// Single instance protection
	lock, err := util.AcquireLock("aetherd")
	if err != nil {
		log.Printf("----------------------------------------------------------------")
		log.Printf("ERROR: Could not start Aether-Realist Core.")
		log.Printf("Detail: %v", err)
		log.Printf("")
		log.Printf("If no other instance is running, please manually delete:")
		log.Printf("%s", filepath.Join(os.TempDir(), "aetherd.lock"))
		log.Printf("----------------------------------------------------------------")
		
		// Don't use log.Fatalf to avoid confusing stack traces, just exit
		time.Sleep(3 * time.Second)
		os.Exit(1)
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

	// Load persisted config and combine with flags
	cm, err := core.NewConfigManager()
	var debugLog *os.File
	if err == nil {
		logPath := filepath.Join(filepath.Dir(cm.GetConfigPath()), "core-debug.log")
		debugLog, _ = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	}

	// Create Core
	c := core.New()

	// Redirect logs to stdout, Core event stream, and optionally a file
	logWriters := []io.Writer{os.Stdout, c.GetLogWriter()}
	if debugLog != nil {
		logWriters = append(logWriters, debugLog)
	}
	log.SetOutput(io.MultiWriter(logWriters...))
	if debugLog != nil {
		defer debugLog.Close()
	}

	log.Println("Starting Aether-Realist Daemon...")
	
	// Force disable system proxy on startup to prevent ghost state from previous crashes
	if err := systemproxy.DisableProxy(); err != nil {
		log.Printf("Warning: failed to clear system proxy: %v", err)
	}

	// Prepare config
	config := core.SessionConfig{
		ListenAddr:    *listenAddr,
		HttpProxyAddr: *httpAddr,
		URL:           *url,
		PSK:           *psk,
	}

	if cm != nil {
		if loaded, err := cm.Load(); err == nil {
			log.Printf("Loaded configuration from %s", cm.GetConfigPath())
			config = *loaded
			
			// Override with flags if explicitly provided
			if *url != "" {
				config.URL = *url
			}
			if *psk != "" {
				config.PSK = *psk
			}
			if *listenAddr != "127.0.0.1:1080" {
				config.ListenAddr = *listenAddr
			}
			if *httpAddr != "" {
				config.HttpProxyAddr = *httpAddr
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
