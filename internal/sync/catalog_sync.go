package sync

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"time"

	"slotegrator-service/internal/domain"
	"slotegrator-service/internal/repository/postgres"
	"slotegrator-service/internal/slotegratorapi"
)

type CatalogSync struct {
	repo     *postgres.Repository
	api      *slotegratorapi.Client
	interval time.Duration
}

func NewCatalogSync(repo *postgres.Repository, api *slotegratorapi.Client, interval time.Duration) *CatalogSync {
	return &CatalogSync{repo: repo, api: api, interval: interval}
}

func (s *CatalogSync) Run(ctx context.Context) {
	s.syncOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnce(ctx)
		}
	}
}

func (s *CatalogSync) syncOnce(ctx context.Context) {
	games, err := s.api.ListGames(ctx)
	if err != nil {
		log.Printf("catalog sync error: %v", err)
		return
	}

	providersMap := make(map[int]domain.Provider)
	domainGames := make([]domain.Game, 0, len(games))
	for _, item := range games {
		if item.ProviderID != 0 {
			providersMap[item.ProviderID] = domain.Provider{
				ID:     item.ProviderID,
				Name:   item.Provider,
				Label:  item.Label,
				Status: "active",
				Source: "slotegrator",
			}
		}

		rtp, volatility := parseParameters(item.Parameters)
		domainGames = append(domainGames, domain.Game{
			UUID:         item.UUID,
			ProviderID:   item.ProviderID,
			Name:         item.Name,
			Type:         item.Type,
			ProviderName: item.Provider,
			Technology:   item.Technology,
			HasLobby:     item.HasLobby == 1,
			IsMobile:     item.IsMobile == 1,
			HasFreespins: item.HasFreespins == 1,
			HasTables:    item.HasTables == 1,
			Label:        item.Label,
			Image:        item.Image,
			RTP:          rtp,
			Volatility:   volatility,
			Tags:         item.Tags,
			Parameters:   item.Parameters,
			Images:       item.Images,
			RelatedGames: item.RelatedGames,
			Status:       "active",
		})
	}

	providers := make([]domain.Provider, 0, len(providersMap))
	for _, p := range providersMap {
		providers = append(providers, p)
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].ID < providers[j].ID
	})

	if err := s.repo.WithTx(ctx, func(ctx context.Context, txRepo *postgres.Repository) error {
		if err := txRepo.UpsertProviders(ctx, providers); err != nil {
			return err
		}
		if err := txRepo.UpsertGames(ctx, domainGames); err != nil {
			return err
		}
		return txRepo.UpsertSyncState(ctx, "catalog", time.Now().UTC(), "")
	}); err != nil {
		log.Printf("catalog sync db error: %v", err)
		return
	}
	log.Printf("catalog sync completed: providers=%d games=%d", len(providers), len(domainGames))
}

func parseParameters(raw json.RawMessage) (*float64, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, ""
	}
	var rtp *float64
	if value, ok := params["rtp"]; ok {
		switch v := value.(type) {
		case float64:
			rtp = &v
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				rtp = &parsed
			}
		}
	}
	volatility := ""
	if value, ok := params["volatility"]; ok {
		if v, ok := value.(string); ok {
			volatility = v
		}
	}
	return rtp, volatility
}
