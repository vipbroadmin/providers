package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"slotegrator-service/internal/config"
	httpdelivery "slotegrator-service/internal/http"
	"slotegrator-service/internal/repository/postgres"
	"slotegrator-service/internal/slotegratorapi"
	catalogsync "slotegrator-service/internal/sync"
	"slotegrator-service/internal/wallets"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config error: %v", err)
	}

	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 30*time.Second)
	if err := postgres.RunMigrations(migrateCtx, cfg.DatabaseURL, cfg.MigrationsDir); err != nil {
		cancelMigrate()
		log.Fatalf("migrations error: %v", err)
	}
	cancelMigrate()

	repo, err := postgres.NewRepository(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("repository error: %v", err)
	}
	defer repo.Close()

	apiClient := slotegratorapi.NewClient(cfg.APIURL, cfg.MerchantID, cfg.MerchantKey)
	walletsClient := wallets.NewClient(cfg.WalletsServiceURL)
	handler := httpdelivery.New(cfg, walletsClient, repo, apiClient)

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("slotegrator-service started on :%s", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http error: %v", err)
		}
	}()

	if cfg.SyncInterval > 0 {
		syncer := catalogsync.NewCatalogSync(repo, apiClient, cfg.SyncInterval)
		go syncer.Run(ctx)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
