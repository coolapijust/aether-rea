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
)

func main() {
	var (
		listenAddr = flag.String("listen", "127.0.0.1:1080", "SOCKS5 listen address")
		apiAddr    = flag.String("api", "127.0.0.1:9880", "HTTP API listen address")
		configFile = flag.String("config", "", "Config file path")
	)
	flag.Parse()

	// Create Core
	c := core.New()

	// Redirect logs to both stdout and Core event stream
	log.SetOutput(io.MultiWriter(os.Stdout, c.GetLogWriter()))

	log.Println("Starting Aether-Realist Daemon...")

	// Prepare config
	config := core.SessionConfig{
		ListenAddr: *listenAddr,
	}

	// Load config if specified
	if *configFile != "" {
		// TODO: Load from file
		log.Printf("Loading config from %s", *configFile)
	}

	// Start Core with config
	if err := c.Start(config); err != nil {
		log.Fatalf("Failed to start core: %v", err)
	}

	// Start HTTP API server
	server := api.NewServer(c, *apiAddr)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
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
