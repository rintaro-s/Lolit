package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lolit/lolit-server/internal/api"
	"github.com/lolit/lolit-server/internal/config"
	"github.com/lolit/lolit-server/internal/db"
	"github.com/lolit/lolit-server/internal/search"
	"github.com/lolit/lolit-server/internal/webhook"
	"github.com/lolit/lolit-server/internal/ws"
)

//go:embed web/webui/templates/* web/webui/static/*
var webFS embed.FS

func main() {
	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		log.Fatalf("ensure dirs: %v", err)
	}

	store, err := db.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer store.Close()

	idx, err := search.New(cfg.IndexPath)
	if err != nil {
		log.Fatalf("open search index: %v", err)
	}
	defer idx.Close()

	hub := ws.NewHub()

	webHandler := &webhook.Handler{
		Store:    store,
		Search:   idx,
		Hub:      hub,
		RepoRoot: cfg.ReposRoot,
	}

	apiHandler := &api.Handler{
		Store:    store,
		Search:   idx,
		Hub:      hub,
		RepoRoot: cfg.ReposRoot,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.Handle("/static/", http.FileServer(getWebFS()))
	mux.HandleFunc("/dashboard-stats", apiHandler.DashboardStats)
	mux.Handle("/ws", hub)
	mux.Handle("/webhook", webHandler)
	apiHandler.Register(mux)

	log.Printf("Lolit Metadata Server listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func getWebFS() http.FileSystem {
	// Prefer local directory for development, otherwise embedded FS.
	if _, err := os.Stat("web/webui/templates/index.html"); err == nil {
		return http.Dir("web/webui")
	}
	sub, err := fs.Sub(webFS, "web/webui")
	if err != nil {
		log.Fatalf("embed fs: %v", err)
	}
	return http.FS(sub)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Try local file first.
	path := filepath.Join("web", "webui", "templates", "index.html")
	if _, err := os.Stat(path); err == nil {
		http.ServeFile(w, r, path)
		return
	}
	b, err := webFS.ReadFile("web/webui/templates/index.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("read index: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
