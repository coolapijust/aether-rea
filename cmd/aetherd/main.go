// aetherd - Aether-Realist Core Daemon
// Provides SOCKS5 proxy with WebTransport backend and HTTP API for GUI
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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
		addr       = flag.String("server-addr", "", "WebTransport server address (domain/IP)")
		port       = flag.Int("server-port", 443, "WebTransport server port")
		path       = flag.String("server-path", "/aether", "WebTransport server path")
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
	// Apply global filters for noise reduction
	filters := []string{"metrics.snapshot", "metricshistory", "pong", "type\":\"pong\""}
	
	stdoutFiltered := util.NewFilteredWriter(os.Stdout, filters)
	coreLogFiltered := util.NewFilteredWriter(c.GetLogWriter(), filters)
	
	logWriters := []io.Writer{stdoutFiltered, coreLogFiltered}
	perfWriter := newPerfLogFileWriter(c, cm)
	logWriters = append(logWriters, perfWriter)
	if debugLog != nil {
		debugLogFiltered := util.NewFilteredWriter(debugLog, filters)
		logWriters = append(logWriters, debugLogFiltered)
	}
	log.SetOutput(io.MultiWriter(logWriters...))
	if debugLog != nil {
		defer debugLog.Close()
	}
	defer perfWriter.Close()

	log.Println("Starting Aether-Realist Daemon...")
	
	// Force disable system proxy on startup to prevent ghost state from previous crashes
	if err := systemproxy.DisableProxy(); err != nil {
		log.Printf("Warning: failed to clear system proxy: %v", err)
	}

	// Prepare config
	config := core.SessionConfig{
		ListenAddr:    *listenAddr,
		HttpProxyAddr: *httpAddr,
		ServerAddr:    *addr,
		ServerPort:    *port,
		ServerPath:    *path,
		PSK:           *psk,
	}

	if cm != nil {
		if loaded, err := cm.Load(); err == nil {
			log.Printf("Loaded configuration from %s", cm.GetConfigPath())
			config = *loaded
			
			// Override with flags if explicitly provided
			if *addr != "" {
				config.ServerAddr = *addr
			}
			if *port != 443 {
				config.ServerPort = *port
			}
			if *path != "/aether" && *path != "" {
				config.ServerPath = *path
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
		} else {
			log.Printf("Warning: Failed to load configuration from %s: %v", cm.GetConfigPath(), err)
		}
	}

	// Enforce default HTTP proxy address if still empty (critical for system proxy on Windows)
	if config.HttpProxyAddr == "" {
		config.HttpProxyAddr = "127.0.0.1:1081"
		log.Printf("Warning: HTTP proxy address not set, defaulting to %s", config.HttpProxyAddr)
	}

	// Start HTTP API server
	server := api.NewServer(c, *apiAddr)
	if err := server.Start(); err != nil {
		log.Printf("Failed to start API server: %v", err)
		return
	}
	log.Printf("HTTP API listening on %s", server.Addr())

	// Start Core with config in background or blocking?
	// The original logic was blocking, let's keep it blocking but after API starts
	if err := c.Start(config); err != nil {
		log.Printf("Warning: Initial core start failed: %v", err)
		log.Printf("You can still reconfigure via GUI as the API server is running.")
	}


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

type perfLogFileWriter struct {
	core     *core.Core
	cm       *core.ConfigManager
	mu       sync.Mutex
	file     *os.File
	filePath string
}

func newPerfLogFileWriter(c *core.Core, cm *core.ConfigManager) *perfLogFileWriter {
	return &perfLogFileWriter{core: c, cm: cm}
}

func (w *perfLogFileWriter) Write(p []byte) (int, error) {
	msg := string(p)
	if !(strings.Contains(msg, "[PERF]") || strings.Contains(msg, "[PERF-GW]") || strings.Contains(msg, "[PERF-GW2]")) {
		return len(p), nil
	}

	cfg := w.core.GetActiveConfig()
	if cfg == nil || !cfg.PerfCaptureEnabled {
		return len(p), nil
	}
	if cfg.PerfCaptureOnConnect && w.core.GetState() != "Active" {
		return len(p), nil
	}

	logPath := strings.TrimSpace(cfg.PerfLogPath)
	if logPath == "" {
		logPath = "logs/perf/client-perf.log"
	}
	if !filepath.IsAbs(logPath) && w.cm != nil {
		baseDir := filepath.Dir(w.cm.GetConfigPath())
		logPath = filepath.Join(baseDir, logPath)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil || w.filePath != logPath {
		if w.file != nil {
			_ = w.file.Close()
			w.file = nil
			w.filePath = ""
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			return len(p), nil
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return len(p), nil
		}
		w.file = f
		w.filePath = logPath
	}

	if w.file != nil {
		_, _ = w.file.Write(p)
	}
	return len(p), nil
}

func (w *perfLogFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	w.filePath = ""
	return err
}
