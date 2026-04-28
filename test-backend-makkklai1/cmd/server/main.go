package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"room-booking/internal/app"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	databaseURL := getenv("DATABASE_URL", "postgres://booking:booking@localhost:5432/booking?sslmode=disable")
	jwtSecret := getenv("JWT_SECRET", "dev-secret-change-in-production")
	httpAddr := getenv("HTTP_ADDR", ":8080")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := app.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := app.NewServer(app.NewStore(pool), jwtSecret)
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch

	shctx, shcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shcancel()
	_ = httpServer.Shutdown(shctx)
}
