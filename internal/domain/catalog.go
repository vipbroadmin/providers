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
	Key              string
	Status           string
	CurrentRunID      string
	CurrentStartedAt *time.Time
	LastSyncAt       *time.Time
	LastSuccessAt    *time.Time
	LastCursor       string
	LastError        string
}

type SyncStatusItem struct {
	Key              string
	Status           string
	CurrentRunID      string
	CurrentStartedAt *time.Time
	LastSuccessAt    *time.Time
	LastCursor       string
	LastError        string
}

type SyncRun struct {
	ID         int64      `json:"id"`
	Key        string     `json:"key"`
	RunID      string     `json:"run_id"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	LastCursor string     `json:"last_cursor,omitempty"`
	Error      string     `json:"error,omitempty"`
	SyncType   string     `json:"sync_type"`
	ItemsCount *int       `json:"count,omitempty"`
}
