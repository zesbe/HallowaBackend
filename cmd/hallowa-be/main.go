package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zesbe/HallowaBackend/internal/config"
	"github.com/zesbe/HallowaBackend/internal/httpserver"
	"github.com/zesbe/HallowaBackend/internal/logger"
	"github.com/zesbe/HallowaBackend/internal/server"
	wastore "github.com/zesbe/HallowaBackend/internal/store"
	"github.com/zesbe/HallowaBackend/internal/supabase"
	"github.com/zesbe/HallowaBackend/internal/whatsapp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.LogLevel)
	log.Info("starting hallowa-be",
		"server_name", cfg.ServerName,
		"server_url", cfg.ServerURL,
		"http", cfg.HTTPListen,
	)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sb := supabase.New(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)

	identity, err := server.Register(rootCtx, sb, cfg, log)
	if err != nil {
		log.Error("register server failed", "err", err)
		os.Exit(1)
	}

	storeContainer, err := wastore.Open(rootCtx, cfg.StoreDBPath)
	if err != nil {
		log.Error("open store failed", "err", err)
		os.Exit(1)
	}
	defer storeContainer.Close()

	mgr := whatsapp.New(storeContainer, sb, log)

	// HTTP server
	httpSrv := httpserver.New(mgr, cfg.InternalAPIKey, log)
	go func() {
		if err := httpSrv.ListenAndServe(cfg.HTTPListen); err != nil {
			log.Error("http server stopped", "err", err)
		}
	}()

	// Device polling loop
	go runDevicePoller(rootCtx, sb, mgr, identity, log)

	// Health heartbeat
	go runHealthBeat(rootCtx, sb, mgr, identity, log)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutdown signal received")
	cancel()
	mgr.Shutdown()
	time.Sleep(500 * time.Millisecond)
}

// runDevicePoller mirrors Node's checkDevices() loop.
// Every 10s: claim unassigned devices, then connect any 'connecting' device assigned to us.
func runDevicePoller(ctx context.Context, sb *supabase.Client, mgr *whatsapp.Manager, id *server.Identity, log *slog.Logger) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	tick := func() {
		// 1. claim unassigned
		unassigned, err := sb.FetchUnassignedDevices(ctx, []string{"connecting", "connected"})
		if err != nil {
			log.Warn("fetch unassigned failed", "err", err)
		} else {
			for _, d := range unassigned {
				ok, err := sb.AtomicAssignDevice(ctx, d.ID, id.ID)
				if err != nil {
					log.Warn("claim device failed", "device_id", d.ID, "err", err)
					continue
				}
				if ok {
					log.Info("claimed device", "device_id", d.ID, "name", d.DeviceName)
				}
			}
		}

		// 2. connect ours
		mine, err := sb.FetchAssignedDevices(ctx, id.ID, []string{"connecting", "connected"})
		if err != nil {
			log.Warn("fetch assigned failed", "err", err)
			return
		}
		for _, d := range mine {
			if mgr.Has(d.ID) {
				continue
			}
			log.Info("starting connection", "device_id", d.ID, "name", d.DeviceName, "method", d.ConnectionMethod, "status", d.Status)
			if err := mgr.Connect(ctx, d); err != nil {
				log.Error("connect failed", "device_id", d.ID, "err", err)
				_ = sb.SetDisconnected(ctx, d.ID, "connect: "+err.Error())
			}
		}
	}

	tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// runHealthBeat updates backend_servers.last_health_check + current_load every 60s.
func runHealthBeat(ctx context.Context, sb *supabase.Client, mgr *whatsapp.Manager, id *server.Identity, log *slog.Logger) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	beat := func() {
		if err := sb.UpdateServerHealth(ctx, id.ID, true, mgr.ActiveCount()); err != nil {
			log.Warn("health update failed", "err", err)
		}
	}
	beat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			beat()
		}
	}
}
