package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"movieflow/api/internal/httpapi"
	"movieflow/api/internal/schedule"
)

func main() {
	port := envOrDefault("PORT", "8080")
	webOrigin := envOrDefault("WEB_ORIGIN", "http://localhost:3000")

	service, err := schedule.NewService()
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.NewHandler(service, webOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("API MovieFlow à l'écoute sur http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
