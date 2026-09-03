// Command server is the entry point for the RevGuard backend API.
package main

import (
	"log"
	"net/http"

	"revguard/backend/internal/config"
	revguardhttp "revguard/backend/internal/http"
)

func main() {
	cfg := config.Load()

	router := revguardhttp.NewRouter()

	addr := ":" + cfg.BackendPort
	log.Printf("revguard backend listening on %s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
