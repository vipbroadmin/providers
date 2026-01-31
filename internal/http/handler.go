package httpdelivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"slotegrator-service/internal/auth"
	"slotegrator-service/internal/config"
	"slotegrator-service/internal/domain"
	"slotegrator-service/internal/repository/postgres"
	"slotegrator-service/internal/slotegratorapi"
	catalogsync "slotegrator-service/internal/sync"
	"slotegrator-service/internal/wallets"

	"github.com/google/uuid"
)

type Handler struct {
	cfg     config.Config
	wallets *wallets.Client
	repo    *postgres.Repository
	api     *slotegratorapi.Client
}

func New(cfg config.Config, walletsClient *wallets.Client, repo *postgres.Repository, api *slotegratorapi.Client) *Handler {
	return &Handler{
		cfg:     cfg,
		wallets: walletsClient,
		repo:    repo,
		api:     api,
	}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/slotegrator/callback", h.callback)
	r.Get("/slotegrator/providers", h.listProviders)
	r.Get("/slotegrator/providers/sync/status", h.providersSyncStatus)
	r.Post("/slotegrator/providers/sync", h.syncProviders)
	r.Get("/slotegrator/games", h.listGames)
	r.Get("/slotegrator/games/sync/status", h.gamesSyncStatus)
	r.Post("/slotegrator/games/sync", h.syncGames)
	r.Post("/slotegrator/launch", h.launch)
	return r
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "method not allowed"))
		return
	}

	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "bad form"))
		return
	}

	headers := map[string]string{
		"X-Merchant-Id": r.Header.Get("X-Merchant-Id"),
		"X-Timestamp":  r.Header.Get("X-Timestamp"),
		"X-Nonce":      r.Header.Get("X-Nonce"),
	}
	xSign := r.Header.Get("X-Sign")

	if headers["X-Merchant-Id"] == "" || headers["X-Timestamp"] == "" || headers["X-Nonce"] == "" || xSign == "" {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "missing auth headers"))
		return
	}
	if headers["X-Merchant-Id"] != h.cfg.MerchantID {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "invalid merchant id"))
		return
	}

	ts, err := strconv.ParseInt(headers["X-Timestamp"], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "invalid timestamp"))
		return
	}
	now := config.NowUnix()
	if h.cfg.MaxTimestampSkewSecs > 0 && abs64(now-ts) > h.cfg.MaxTimestampSkewSecs {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "timestamp expired"))
		return
	}

	params := map[string]string{}
	for key, vals := range r.PostForm {
		if len(vals) > 0 {
			params[key] = vals[0]
		}
	}
	for k, v := range headers {
		params[k] = v
	}

	expected := auth.BuildSign(params, h.cfg.MerchantKey)
	if !auth.EqualSign(xSign, expected) {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "invalid sign"))
		return
	}

	h.touchSessionFromCallback(r)

	action := strings.ToLower(r.FormValue("action"))
	switch action {
	case "balance":
		h.handleBalance(w, r)
	case "bet":
		h.handleBet(w, r)
	case "win":
		h.handleWin(w, r)
	case "refund":
		h.handleRefund(w, r)
	case "rollback":
		h.handleRollback(w, r)
	default:
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "unknown action"))
	}
}

func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	playerID := r.FormValue("player_id")
	currency := r.FormValue("currency")
	if playerID == "" || currency == "" {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "missing player_id/currency"))
		return
	}
	resp, err := h.wallets.GetBalance(r.Context(), playerID, currency)
	if err != nil {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "wallets error"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance":        resp.Balance,
		"transaction_id": r.FormValue("transaction_id"),
	})
}

func (h *Handler) handleBet(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, "bet")
}

func (h *Handler) handleWin(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, "win")
}

func (h *Handler) handleRefund(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, "refund")
}

func (h *Handler) handleRollback(w http.ResponseWriter, r *http.Request) {
	h.handleAction(w, r, "rollback")
}

func (h *Handler) handleAction(w http.ResponseWriter, r *http.Request, action string) {
	req, err := parseGameRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "bad request"))
		return
	}

	resp, err := h.wallets.PostAction(r.Context(), action, req)
	if err != nil {
		if errors.Is(err, wallets.ErrInsufficientFunds) {
			writeJSON(w, http.StatusOK, errorResponse("INSUFFICIENT_FUNDS", "not enough money"))
			return
		}
		writeJSON(w, http.StatusOK, errorResponse("INTERNAL_ERROR", "wallets error"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance":        resp.Balance,
		"transaction_id": resp.TransactionID,
	})
}

var errBadRequest = errors.New("bad_request")

func parseGameRequest(r *http.Request) (wallets.GameRequest, error) {
	req := wallets.GameRequest{
		PlayerID:      r.FormValue("player_id"),
		Currency:      r.FormValue("currency"),
		TransactionID: r.FormValue("transaction_id"),
		RoundID:       r.FormValue("round_id"),
		SessionID:     r.FormValue("session_id"),
		Type:          r.FormValue("type"),
		Amount:        r.FormValue("amount"),
	}
	req.Currency = strings.ToUpper(req.Currency)
	if req.PlayerID == "" || req.Currency == "" || req.TransactionID == "" || req.Amount == "" {
		return wallets.GameRequest{}, errBadRequest
	}
	return req, nil
}

type launchRequest struct {
	GameUUID  string `json:"game_uuid"`
	PlayerID  string `json:"player_id"`
	PlayerName string `json:"player_name"`
	Currency  string `json:"currency"`
	SessionID string `json:"session_id,omitempty"`
	Device    string `json:"device,omitempty"`
	ReturnURL string `json:"return_url,omitempty"`
	Language  string `json:"language,omitempty"`
	Email     string `json:"email,omitempty"`
	LobbyData string `json:"lobby_data,omitempty"`
	Demo      bool   `json:"demo,omitempty"`
}

type launchResponse struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

func (h *Handler) launch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req launchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.GameUUID == "" || req.PlayerID == "" || req.PlayerName == "" || req.Currency == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}
	req.Currency = strings.ToUpper(req.Currency)
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
	}

	game, err := h.repo.GetGameByUUID(r.Context(), req.GameUUID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "game not found"})
		return
	}

	url, err := h.api.InitGame(r.Context(), slotegratorapi.InitGameRequest{
		GameUUID:  req.GameUUID,
		PlayerID:  req.PlayerID,
		PlayerName: req.PlayerName,
		Currency:  req.Currency,
		SessionID: req.SessionID,
		Device:    req.Device,
		ReturnURL: req.ReturnURL,
		Language:  req.Language,
		Email:     req.Email,
		LobbyData: req.LobbyData,
		Demo:      req.Demo,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "launch failed"})
		return
	}

	providerID := &game.ProviderID

	if err := h.repo.UpsertSession(r.Context(), domain.GameSession{
		SessionID:  req.SessionID,
		PlayerID:   req.PlayerID,
		ProviderID: providerID,
		GameUUID:   req.GameUUID,
		Currency:   req.Currency,
		Status:     "active",
		LaunchURL:  url,
		Device:     req.Device,
		ReturnURL:  req.ReturnURL,
		Language:   req.Language,
	}); err != nil {
		log.Printf("session save error: %v", err)
	}

	writeJSON(w, http.StatusOK, launchResponse{
		SessionID: req.SessionID,
		URL:       url,
	})
}

func (h *Handler) listProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	filter := postgres.ProviderFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
		Limit:  parseInt(r.URL.Query().Get("limit")),
		Offset: parseInt(r.URL.Query().Get("offset")),
	}
	items, err := h.repo.ListProviders(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load providers"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) listGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	filter := postgres.GameFilter{
		ProviderID: parseInt(r.URL.Query().Get("provider_id")),
		Status:     r.URL.Query().Get("status"),
		Search:     r.URL.Query().Get("search"),
		Limit:      parseInt(r.URL.Query().Get("limit")),
		Offset:     parseInt(r.URL.Query().Get("offset")),
	}
	items, err := h.repo.ListGames(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load games"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type gamesSyncRequest struct {
	ProviderIDs []int `json:"provider_ids"`
}

type syncStartResponse struct {
	RunID     string     `json:"run_id"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

type syncErrorDetails struct {
	Message string              `json:"message"`
	Headers map[string][]string `json:"headers"`
	Query   map[string][]string `json:"query"`
	Body    any                 `json:"body"`
}

type syncErrorResponse struct {
	Error   string           `json:"error"`
	Details syncErrorDetails `json:"details"`
}

func (h *Handler) syncProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	start, err := h.repo.StartSync(r.Context(), "providers", "")
	if err != nil {
		log.Printf("providers sync start error: %v", err)
		writeSyncError(w, http.StatusInternalServerError, "sync start failed", err, r, map[string]any{})
		return
	}
	startedAt := optionalTime(start.StartedAt)
	writeJSON(w, http.StatusAccepted, syncStartResponse{
		RunID:     start.RunID,
		Status:    "running",
		StartedAt: startedAt,
	})
	if start.AlreadyRunning {
		return
	}

	go h.runProvidersSync(start.RunID)
}

func (h *Handler) syncGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req gamesSyncRequest
	rawBody, err := readJSONBody(r, &req)
	if err != nil {
		body := formatRawBody(rawBody)
		writeSyncError(w, http.StatusBadRequest, "invalid request", err, r, body)
		return
	}
	providerIDs, err := normalizeProviderIDs(req.ProviderIDs)
	if err != nil {
		writeSyncError(w, http.StatusBadRequest, "invalid provider_ids", err, r, req)
		return
	}
	cursor := formatProviderCursor(providerIDs)
	start, err := h.repo.StartSync(r.Context(), "games", cursor)
	if err != nil {
		log.Printf("games sync start error: %v", err)
		writeSyncError(w, http.StatusInternalServerError, "sync start failed", err, r, req)
		return
	}
	startedAt := optionalTime(start.StartedAt)
	writeJSON(w, http.StatusAccepted, syncStartResponse{
		RunID:     start.RunID,
		Status:    "running",
		StartedAt: startedAt,
	})
	if start.AlreadyRunning {
		return
	}

	go h.runGamesSync(start.RunID, providerIDs)
}

func (h *Handler) providersSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limitRaw := parseInt(r.URL.Query().Get("limit"))
	offsetRaw := parseInt(r.URL.Query().Get("offset"))
	limit := normalizeLimit(limitRaw)
	offset := normalizeOffset(offsetRaw)
	items, err := h.repo.ListSyncRuns(r.Context(), "providers", limit, offset)
	if err != nil {
		log.Printf("providers sync status error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load sync status"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) gamesSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limitRaw := parseInt(r.URL.Query().Get("limit"))
	offsetRaw := parseInt(r.URL.Query().Get("offset"))
	limit := normalizeLimit(limitRaw)
	offset := normalizeOffset(offsetRaw)
	items, err := h.repo.ListSyncRuns(r.Context(), "games", limit, offset)
	if err != nil {
		log.Printf("games sync status error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load sync status"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"limit":  limit,
		"offset": offset,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func errorResponse(code, desc string) map[string]any {
	return map[string]any{
		"error_code":        code,
		"error_description": desc,
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (h *Handler) touchSessionFromCallback(r *http.Request) {
	sessionID := r.FormValue("session_id")
	gameUUID := r.FormValue("game_uuid")
	if sessionID == "" || gameUUID == "" {
		return
	}
	playerID := r.FormValue("player_id")
	currency := strings.ToUpper(r.FormValue("currency"))
	roundID := r.FormValue("round_id")
	transactionID := r.FormValue("transaction_id")
	if err := h.repo.TouchSessionFromCallback(r.Context(), sessionID, playerID, gameUUID, currency, roundID, transactionID); err != nil {
		log.Printf("session touch error: %v", err)
	}
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("invalid json")
	}
	return nil
}

func readJSONBody(r *http.Request, out any) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return raw, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return raw, err
	}
	if decoder.More() {
		return raw, errors.New("invalid json")
	}
	return raw, nil
}

func parseInt(raw string) int {
	if raw == "" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return val
}

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func normalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func normalizeProviderIDs(ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	unique := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errBadRequest
		}
		unique[id] = struct{}{}
	}
	normalized := make([]int, 0, len(unique))
	for id := range unique {
		normalized = append(normalized, id)
	}
	sort.Ints(normalized)
	return normalized, nil
}

func formatProviderCursor(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func (h *Handler) runProvidersSync(runID string) {
	ctx := context.Background()
	syncer := catalogsync.NewCatalogSync(h.repo, h.api, 0)
	_, err := syncer.SyncProviders(ctx)
	if finishErr := h.repo.FinishSync(ctx, "providers", runID, err == nil, errString(err)); finishErr != nil {
		log.Printf("providers sync finish error: %v", finishErr)
	}
	if err != nil {
		log.Printf("providers sync error: %v", err)
	}
}

func (h *Handler) runGamesSync(runID string, providerIDs []int) {
	ctx := context.Background()
	syncer := catalogsync.NewCatalogSync(h.repo, h.api, 0)
	_, err := syncer.SyncGames(ctx, providerIDs)
	if finishErr := h.repo.FinishSync(ctx, "games", runID, err == nil, errString(err)); finishErr != nil {
		log.Printf("games sync finish error: %v", finishErr)
	}
	if err != nil {
		log.Printf("games sync error: %v", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func writeSyncError(w http.ResponseWriter, status int, message string, err error, r *http.Request, body any) {
	writeJSON(w, status, syncErrorResponse{
		Error: message,
		Details: syncErrorDetails{
			Message: errString(err),
			Headers: sanitizeHeaders(r.Header),
			Query:   r.URL.Query(),
			Body:    body,
		},
	})
}

func sanitizeHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if strings.EqualFold(key, "Authorization") {
			out[key] = []string{"REDACTED"}
			continue
		}
		out[key] = values
	}
	return out
}

func formatRawBody(raw []byte) map[string]any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}
	}
	return map[string]any{"raw": string(raw)}
}

 
