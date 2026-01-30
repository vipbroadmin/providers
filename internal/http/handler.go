package httpdelivery

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"slotegrator-service/internal/auth"
	"slotegrator-service/internal/config"
	"slotegrator-service/internal/domain"
	"slotegrator-service/internal/repository/postgres"
	"slotegrator-service/internal/slotegratorapi"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/slotegrator/callback", h.callback)
	mux.HandleFunc("/slotegrator/providers", h.listProviders)
	mux.HandleFunc("/slotegrator/games", h.listGames)
	mux.HandleFunc("/slotegrator/launch", h.launch)
	return mux
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

 
