package domain

import (
	"encoding/json"
	"time"
)

type Provider struct {
	ID        int
	Name      string
	Label     string
	Status    string
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Game struct {
	UUID         string
	ProviderID   int
	Name         string
	Type         string
	ProviderName string
	Technology   string
	HasLobby     bool
	IsMobile     bool
	HasFreespins bool
	HasTables    bool
	Label        string
	Image        string
	RTP          *float64
	Volatility   string
	Tags         json.RawMessage
	Parameters   json.RawMessage
	Images       json.RawMessage
	RelatedGames json.RawMessage
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type GameSession struct {
	SessionID        string
	PlayerID         string
	ProviderID       *int
	GameUUID         string
	Currency         string
	Status           string
	LaunchURL        string
	Device           string
	ReturnURL        string
	Language         string
	LastRoundID      string
	LastTransactionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	EndedAt          *time.Time
}

type SyncState struct {
	Key        string
	LastSyncAt *time.Time
	LastCursor string
}
