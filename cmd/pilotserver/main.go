package main

import (
	"log"
	"net/http"

	"pilotserver/internal/adminapi"
	"pilotserver/internal/api"
	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.New(st, cfg).Mount(mux)
	hub := athena.NewHub(cfg)
	athena.Mount(mux, hub, cfg)
	adminapi.Mount(mux, st, hub, cfg, adminPasswordHash)
	log.Printf("listening on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, mux))
}
