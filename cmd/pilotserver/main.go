package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"pilotserver/internal/adminapi"
	"pilotserver/internal/api"
	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/billing"
	"pilotserver/internal/config"
	"pilotserver/internal/listenaddr"
	"pilotserver/internal/maps"
	"pilotserver/internal/ota"
	"pilotserver/internal/publicbase"
	"pilotserver/internal/replay"
	"pilotserver/internal/store"
	"pilotserver/internal/upload"
	adminweb "pilotserver/web/admin"
)

const mediaTicketTTL = 15 * time.Minute

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	adminPasswordHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	tickets := replay.NewTicketManager(cfg.JWTSecret, mediaTicketTTL)
	locator := replay.NewLocator(cfg.DataDir)
	telemetryCache := replay.NewCache(cfg.DataDir, replay.NewParser())
	replayService := replay.NewServiceWithTelemetry(st, tickets, locator, telemetryCache)

	baseURL, err := publicbase.New(st, cfg.PublicBaseURL)
	if err != nil {
		// Invalid env value must not prevent startup; fix later via /admin/.
		log.Printf("ignoring invalid PILOTSERVER_PUBLIC_BASE_URL %q: %v", cfg.PublicBaseURL, err)
		baseURL, err = publicbase.New(st, "")
		if err != nil {
			log.Fatal(err)
		}
	}
	if baseURL.Get() == "" {
		log.Printf("public base URL not set; configure via /admin/ or PILOTSERVER_PUBLIC_BASE_URL")
	}

	reloadListen := make(chan string, 1)
	listen, err := listenaddr.New(
		cfg.ListenAddr,
		config.EnvFilePath(cfg.DataDir),
		config.AllowNonLoopback(),
		func(addr string) {
			select {
			case reloadListen <- addr:
			default:
				// drop if a reload is already queued
			}
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mountAdminUI(mux)
	api.New(st, cfg, baseURL).Mount(mux)
	upload.Mount(mux, st, cfg)
	replay.NewMediaHandler(tickets, replayService, locator).Mount(mux)
	ota.Mount(mux, cfg)
	billing.Mount(mux)
	maps.Mount(mux)
	hub := athena.NewHub(cfg)
	hub.SetBaseURLProvider(baseURL.Get)
	athena.Mount(mux, hub, st, cfg)
	adminapi.Mount(mux, st, hub, cfg, adminPasswordHash, baseURL, listen, replayService)

	if err := serveWithReload(mux, listen.Get(), reloadListen); err != nil {
		log.Fatal(err)
	}
}

func serveWithReload(handler http.Handler, addr string, reloads <-chan string) error {
	for {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: handler}
		errCh := make(chan error, 1)
		go func() {
			log.Printf("listening on %s", addr)
			errCh <- srv.Serve(ln)
		}()

		select {
		case newAddr := <-reloads:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(ctx)
			cancel()
			if err := <-errCh; err != nil && err != http.ErrServerClosed {
				log.Printf("server stopped with error: %v", err)
			}
			if newAddr == "" || newAddr == addr {
				continue
			}
			log.Printf("listen address changed %s -> %s", addr, newAddr)
			addr = newAddr
		case err := <-errCh:
			if err == nil || err == http.ErrServerClosed {
				return nil
			}
			return err
		}
	}
}

func mountAdminUI(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/", adminweb.ServeHTTP)
}
