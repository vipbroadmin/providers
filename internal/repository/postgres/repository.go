package postgres

import (
	"context"
	"errors"
	"time"

	"slotegrator-service/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
	tx   pgx.Tx
}

func NewRepository(databaseURL string) (*Repository, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *Repository) WithTx(ctx context.Context, fn func(ctx context.Context, txRepo *Repository) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	txRepo := &Repository{pool: r.pool, tx: tx}
	if err := fn(ctx, txRepo); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

type ProviderFilter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type GameFilter struct {
	ProviderID int
	Status     string
	Search     string
	Limit      int
	Offset     int
}

func (r *Repository) UpsertProviders(ctx context.Context, providers []domain.Provider) error {
	if len(providers) == 0 {
		return nil
	}
	const query = `
		INSERT INTO providers (id, name, label, status, source, updated_at, created_at)
		VALUES ($1,$2,$3,$4,$5, now(), now())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			label = EXCLUDED.label,
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			updated_at = now()
	`
	batch := &pgx.Batch{}
	for _, p := range providers {
		status := p.Status
		if status == "" {
			status = "active"
		}
		source := p.Source
		if source == "" {
			source = "slotegrator"
		}
		batch.Queue(query, p.ID, p.Name, nullString(p.Label), status, source)
	}
	return r.execBatch(ctx, batch)
}

func (r *Repository) UpsertGames(ctx context.Context, games []domain.Game) error {
	if len(games) == 0 {
		return nil
	}
	const query = `
		INSERT INTO games (
			game_uuid, provider_id, name, type, provider_name, technology,
			has_lobby, is_mobile, has_freespins, has_tables, label, image,
			rtp, volatility, tags, parameters, images, related_games, status,
			updated_at, created_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,$11,$12,
			$13,$14,$15,$16,$17,$18,$19,
			now(), now()
		)
		ON CONFLICT (game_uuid) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			name = EXCLUDED.name,
			type = EXCLUDED.type,
			provider_name = EXCLUDED.provider_name,
			technology = EXCLUDED.technology,
			has_lobby = EXCLUDED.has_lobby,
			is_mobile = EXCLUDED.is_mobile,
			has_freespins = EXCLUDED.has_freespins,
			has_tables = EXCLUDED.has_tables,
			label = EXCLUDED.label,
			image = EXCLUDED.image,
			rtp = EXCLUDED.rtp,
			volatility = EXCLUDED.volatility,
			tags = EXCLUDED.tags,
			parameters = EXCLUDED.parameters,
			images = EXCLUDED.images,
			related_games = EXCLUDED.related_games,
			status = EXCLUDED.status,
			updated_at = now()
	`
	batch := &pgx.Batch{}
	for _, g := range games {
		status := g.Status
		if status == "" {
			status = "active"
		}
		batch.Queue(query,
			g.UUID,
			g.ProviderID,
			g.Name,
			nullString(g.Type),
			nullString(g.ProviderName),
			nullString(g.Technology),
			g.HasLobby,
			g.IsMobile,
			g.HasFreespins,
			g.HasTables,
			nullString(g.Label),
			nullString(g.Image),
			g.RTP,
			nullString(g.Volatility),
			nullBytes(g.Tags),
			nullBytes(g.Parameters),
			nullBytes(g.Images),
			nullBytes(g.RelatedGames),
			status,
		)
	}
	return r.execBatch(ctx, batch)
}

func (r *Repository) ListProviders(ctx context.Context, filter ProviderFilter) ([]domain.Provider, error) {
	limit, offset := normalizePaging(filter.Limit, filter.Offset)
	const query = `
		SELECT id, name, label, status, source, created_at, updated_at
		FROM providers
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR name ILIKE '%' || $2 || '%')
		ORDER BY name
		LIMIT $3 OFFSET $4
	`
	rows, err := r.query(ctx, query, filter.Status, filter.Search, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Provider
	for rows.Next() {
		var p domain.Provider
		var label *string
		if err := rows.Scan(&p.ID, &p.Name, &label, &p.Status, &p.Source, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if label != nil {
			p.Label = *label
		}
		result = append(result, p)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return result, nil
}

func (r *Repository) ListGames(ctx context.Context, filter GameFilter) ([]domain.Game, error) {
	limit, offset := normalizePaging(filter.Limit, filter.Offset)
	const query = `
		SELECT game_uuid, provider_id, name, type, provider_name, technology,
		       has_lobby, is_mobile, has_freespins, has_tables, label, image,
		       rtp, volatility, tags, parameters, images, related_games, status,
		       created_at, updated_at
		FROM games
		WHERE ($1 = 0 OR provider_id = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR name ILIKE '%' || $3 || '%')
		ORDER BY name
		LIMIT $4 OFFSET $5
	`
	rows, err := r.query(ctx, query, filter.ProviderID, filter.Status, filter.Search, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []domain.Game
	for rows.Next() {
		var g domain.Game
		var (
			gameType     *string
			providerName *string
			technology   *string
			label        *string
			image        *string
			volatility   *string
			rtp          *float64
			tags         []byte
			parameters   []byte
			images       []byte
			related      []byte
		)
		if err := rows.Scan(
			&g.UUID,
			&g.ProviderID,
			&g.Name,
			&gameType,
			&providerName,
			&technology,
			&g.HasLobby,
			&g.IsMobile,
			&g.HasFreespins,
			&g.HasTables,
			&label,
			&image,
			&rtp,
			&volatility,
			&tags,
			&parameters,
			&images,
			&related,
			&g.Status,
			&g.CreatedAt,
			&g.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if gameType != nil {
			g.Type = *gameType
		}
		if providerName != nil {
			g.ProviderName = *providerName
		}
		if technology != nil {
			g.Technology = *technology
		}
		if label != nil {
			g.Label = *label
		}
		if image != nil {
			g.Image = *image
		}
		if volatility != nil {
			g.Volatility = *volatility
		}
		if rtp != nil {
			g.RTP = rtp
		}
		g.Tags = tags
		g.Parameters = parameters
		g.Images = images
		g.RelatedGames = related
		result = append(result, g)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return result, nil
}

func (r *Repository) GetGameByUUID(ctx context.Context, gameUUID string) (domain.Game, error) {
	const query = `
		SELECT game_uuid, provider_id, name, type, provider_name, technology,
		       has_lobby, is_mobile, has_freespins, has_tables, label, image,
		       rtp, volatility, tags, parameters, images, related_games, status,
		       created_at, updated_at
		FROM games
		WHERE game_uuid = $1
	`
	row := r.queryRow(ctx, query, gameUUID)
	var g domain.Game
	var (
		gameType     *string
		providerName *string
		technology   *string
		label        *string
		image        *string
		volatility   *string
		rtp          *float64
		tags         []byte
		parameters   []byte
		images       []byte
		related      []byte
	)
	if err := row.Scan(
		&g.UUID,
		&g.ProviderID,
		&g.Name,
		&gameType,
		&providerName,
		&technology,
		&g.HasLobby,
		&g.IsMobile,
		&g.HasFreespins,
		&g.HasTables,
		&label,
		&image,
		&rtp,
		&volatility,
		&tags,
		&parameters,
		&images,
		&related,
		&g.Status,
		&g.CreatedAt,
		&g.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Game{}, pgx.ErrNoRows
		}
		return domain.Game{}, err
	}
	if gameType != nil {
		g.Type = *gameType
	}
	if providerName != nil {
		g.ProviderName = *providerName
	}
	if technology != nil {
		g.Technology = *technology
	}
	if label != nil {
		g.Label = *label
	}
	if image != nil {
		g.Image = *image
	}
	if volatility != nil {
		g.Volatility = *volatility
	}
	if rtp != nil {
		g.RTP = rtp
	}
	g.Tags = tags
	g.Parameters = parameters
	g.Images = images
	g.RelatedGames = related
	return g, nil
}

func (r *Repository) UpsertSession(ctx context.Context, session domain.GameSession) error {
	status := session.Status
	if status == "" {
		status = "active"
	}
	const query = `
		INSERT INTO game_sessions (
			session_id, player_id, provider_id, game_uuid, currency,
			status, launch_url, device, return_url, language,
			last_round_id, last_transaction_id, updated_at, created_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,$10,
			$11,$12, now(), now()
		)
		ON CONFLICT (session_id) DO UPDATE SET
			player_id = EXCLUDED.player_id,
			provider_id = COALESCE(EXCLUDED.provider_id, game_sessions.provider_id),
			game_uuid = EXCLUDED.game_uuid,
			currency = EXCLUDED.currency,
			status = EXCLUDED.status,
			launch_url = EXCLUDED.launch_url,
			device = EXCLUDED.device,
			return_url = EXCLUDED.return_url,
			language = EXCLUDED.language,
			last_round_id = EXCLUDED.last_round_id,
			last_transaction_id = EXCLUDED.last_transaction_id,
			updated_at = now()
	`
	_, err := r.exec(ctx, query,
		session.SessionID,
		session.PlayerID,
		session.ProviderID,
		session.GameUUID,
		session.Currency,
		status,
		nullString(session.LaunchURL),
		nullString(session.Device),
		nullString(session.ReturnURL),
		nullString(session.Language),
		nullString(session.LastRoundID),
		nullString(session.LastTransactionID),
	)
	return err
}

func (r *Repository) TouchSessionFromCallback(ctx context.Context, sessionID, playerID, gameUUID, currency, roundID, transactionID string) error {
	const query = `
		INSERT INTO game_sessions (
			session_id, player_id, provider_id, game_uuid, currency,
			status, last_round_id, last_transaction_id, updated_at, created_at
		) VALUES (
			$1,$2,(SELECT provider_id FROM games WHERE game_uuid = $3),$3,$4,
			'active',$5,$6, now(), now()
		)
		ON CONFLICT (session_id) DO UPDATE SET
			player_id = COALESCE(EXCLUDED.player_id, game_sessions.player_id),
			provider_id = COALESCE(EXCLUDED.provider_id, game_sessions.provider_id),
			game_uuid = COALESCE(EXCLUDED.game_uuid, game_sessions.game_uuid),
			currency = COALESCE(EXCLUDED.currency, game_sessions.currency),
			last_round_id = COALESCE(EXCLUDED.last_round_id, game_sessions.last_round_id),
			last_transaction_id = COALESCE(EXCLUDED.last_transaction_id, game_sessions.last_transaction_id),
			updated_at = now()
	`
	_, err := r.exec(ctx, query,
		sessionID,
		nullString(playerID),
		gameUUID,
		nullString(currency),
		nullString(roundID),
		nullString(transactionID),
	)
	return err
}

func (r *Repository) UpsertSyncState(ctx context.Context, key string, lastSyncAt time.Time, lastCursor string) error {
	const query = `
		INSERT INTO sync_state (key, last_sync_at, last_cursor)
		VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET
			last_sync_at = EXCLUDED.last_sync_at,
			last_cursor = EXCLUDED.last_cursor
	`
	_, err := r.exec(ctx, query, key, lastSyncAt, nullString(lastCursor))
	return err
}

func (r *Repository) execBatch(ctx context.Context, batch *pgx.Batch) error {
	if batch.Len() == 0 {
		return nil
	}
	var br pgx.BatchResults
	if r.tx != nil {
		br = r.tx.SendBatch(ctx, batch)
	} else {
		br = r.pool.SendBatch(ctx, batch)
	}
	for range batch.QueuedQueries {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	return br.Close()
}

func (r *Repository) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if r.tx != nil {
		return r.tx.Query(ctx, sql, args...)
	}
	return r.pool.Query(ctx, sql, args...)
}

func (r *Repository) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if r.tx != nil {
		return r.tx.QueryRow(ctx, sql, args...)
	}
	return r.pool.QueryRow(ctx, sql, args...)
}

func (r *Repository) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if r.tx != nil {
		return r.tx.Exec(ctx, sql, args...)
	}
	return r.pool.Exec(ctx, sql, args...)
}

func normalizePaging(limit, offset int) (int, int) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
