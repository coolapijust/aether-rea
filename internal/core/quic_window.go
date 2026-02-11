package core

import (
	"fmt"
	"os"
	"strconv"
)

// QUICWindowConfig holds QUIC flow-control window settings.
type QUICWindowConfig struct {
	Profile                        string
	InitialStreamReceiveWindow     uint64
	InitialConnectionReceiveWindow uint64
	MaxStreamReceiveWindow         uint64
	MaxConnectionReceiveWindow     uint64
	OverrideApplied                bool
}

// ResolveQUICWindowConfig resolves profile defaults and optional env overrides.
//
// Env overrides:
// - QUIC_INITIAL_STREAM_RECV_WINDOW
// - QUIC_INITIAL_CONN_RECV_WINDOW
// - QUIC_MAX_STREAM_RECV_WINDOW
// - QUIC_MAX_CONN_RECV_WINDOW
func ResolveQUICWindowConfig(profile string) (QUICWindowConfig, error) {
	cfg := QUICWindowConfig{}
	switch profile {
	case "conservative":
		cfg.Profile = "conservative"
		cfg.InitialStreamReceiveWindow = 512 * 1024
		cfg.InitialConnectionReceiveWindow = 1536 * 1024
		cfg.MaxStreamReceiveWindow = 2 * 1024 * 1024
		cfg.MaxConnectionReceiveWindow = 4 * 1024 * 1024
	case "aggressive":
		cfg.Profile = "aggressive"
		cfg.InitialStreamReceiveWindow = 4 * 1024 * 1024
		cfg.InitialConnectionReceiveWindow = 8 * 1024 * 1024
		cfg.MaxStreamReceiveWindow = 32 * 1024 * 1024
		cfg.MaxConnectionReceiveWindow = 48 * 1024 * 1024
	default:
		cfg.Profile = "normal"
		cfg.InitialStreamReceiveWindow = 2 * 1024 * 1024
		cfg.InitialConnectionReceiveWindow = 3 * 1024 * 1024
		cfg.MaxStreamReceiveWindow = 4 * 1024 * 1024
		cfg.MaxConnectionReceiveWindow = 8 * 1024 * 1024
	}

	var err error
	if cfg.InitialStreamReceiveWindow, err = parseWindowOverride("QUIC_INITIAL_STREAM_RECV_WINDOW", cfg.InitialStreamReceiveWindow); err != nil {
		return cfg, err
	}
	if cfg.InitialConnectionReceiveWindow, err = parseWindowOverride("QUIC_INITIAL_CONN_RECV_WINDOW", cfg.InitialConnectionReceiveWindow); err != nil {
		return cfg, err
	}
	if cfg.MaxStreamReceiveWindow, err = parseWindowOverride("QUIC_MAX_STREAM_RECV_WINDOW", cfg.MaxStreamReceiveWindow); err != nil {
		return cfg, err
	}
	if cfg.MaxConnectionReceiveWindow, err = parseWindowOverride("QUIC_MAX_CONN_RECV_WINDOW", cfg.MaxConnectionReceiveWindow); err != nil {
		return cfg, err
	}

	cfg.OverrideApplied = os.Getenv("QUIC_INITIAL_STREAM_RECV_WINDOW") != "" ||
		os.Getenv("QUIC_INITIAL_CONN_RECV_WINDOW") != "" ||
		os.Getenv("QUIC_MAX_STREAM_RECV_WINDOW") != "" ||
		os.Getenv("QUIC_MAX_CONN_RECV_WINDOW") != ""

	if cfg.InitialStreamReceiveWindow > cfg.MaxStreamReceiveWindow {
		return cfg, fmt.Errorf("invalid QUIC windows: initial stream window > max stream window")
	}
	if cfg.InitialConnectionReceiveWindow > cfg.MaxConnectionReceiveWindow {
		return cfg, fmt.Errorf("invalid QUIC windows: initial connection window > max connection window")
	}

	return cfg, nil
}

func parseWindowOverride(name string, fallback uint64) (uint64, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s parse failed: %w", name, err)
	}
	if parsed < 64*1024 {
		return 0, fmt.Errorf("%s too small: %d", name, parsed)
	}
	if parsed > 256*1024*1024 {
		return 0, fmt.Errorf("%s too large: %d", name, parsed)
	}
	return parsed, nil
}

