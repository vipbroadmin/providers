package slotegratorapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"slotegrator-service/internal/auth"

	"github.com/google/uuid"
)

type Client struct {
	baseURL     string
	merchantID  string
	merchantKey string
	client      *http.Client
}

func NewClient(baseURL, merchantID, merchantKey string) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		merchantID:  merchantID,
		merchantKey: merchantKey,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

type GameItem struct {
	UUID         string          `json:"uuid"`
	Name         string          `json:"name"`
	Image        string          `json:"image"`
	Type         string          `json:"type"`
	Provider     string          `json:"provider"`
	ProviderID   int             `json:"provider_id"`
	Technology   string          `json:"technology"`
	HasLobby     int             `json:"has_lobby"`
	IsMobile     int             `json:"is_mobile"`
	HasFreespins int             `json:"has_freespins"`
	HasTables    int             `json:"has_tables"`
	Label        string          `json:"label"`
	Tags         json.RawMessage `json:"tags"`
	Parameters   json.RawMessage `json:"parameters"`
	Images       json.RawMessage `json:"images"`
	RelatedGames json.RawMessage `json:"related_games"`
}

type gamesResponse struct {
	Items []GameItem `json:"items"`
	Links struct {
		Next *struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
}

type InitGameRequest struct {
	GameUUID   string
	PlayerID   string
	PlayerName string
	Currency   string
	SessionID  string
	Device     string
	ReturnURL  string
	Language   string
	Email      string
	LobbyData  string
	Demo       bool
}

type initGameResponse struct {
	URL string `json:"url"`
}

// LobbyTable represents a single table entry from GET /games/lobby.
type LobbyTable struct {
	LobbyData    string          `json:"lobbyData"`
	Name         string          `json:"name"`
	IsOpen       bool            `json:"isOpen"`
	OpenTime     string          `json:"openTime"`
	CloseTime    string          `json:"closeTime"`
	DealerName   string          `json:"dealerName"`
	DealerAvatar string          `json:"dealerAvatar"`
	Technology   string          `json:"technology"`
	Limits       json.RawMessage `json:"limits"`
	TableID      string          `json:"tableId"`
}

// GetLobby returns lobby tables for the given game (for games with has_lobby).
// technology is optional: "html5" or "flash".
func (c *Client) GetLobby(ctx context.Context, gameUUID, currency, technology string) ([]LobbyTable, error) {
	if gameUUID == "" || currency == "" {
		return nil, fmt.Errorf("game_uuid and currency required")
	}
	params := url.Values{}
	params.Set("game_uuid", gameUUID)
	params.Set("currency", currency)
	if technology != "" {
		params.Set("technology", technology)
	}
	resp, err := c.doRequest(ctx, http.MethodGet, "/games/lobby", params)
	if err != nil {
		return nil, err
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Lobby json.RawMessage `json:"lobby"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	// API may return lobby as array or single object
	var tables []LobbyTable
	if err := json.Unmarshal(parsed.Lobby, &tables); err != nil {
		var single LobbyTable
		if singleErr := json.Unmarshal(parsed.Lobby, &single); singleErr != nil {
			return nil, fmt.Errorf("lobby response: %w", err)
		}
		tables = []LobbyTable{single}
	}
	return tables, nil
}

func (c *Client) ListGames(ctx context.Context) ([]GameItem, error) {
	path := "/games"
	params := url.Values{}
	params.Set("expand", "tags,parameters,images,related_games")

	var all []GameItem
	for {
		resp, err := c.doRequest(ctx, http.MethodGet, path, params)
		if err != nil {
			return nil, err
		}
		body, err := readResponseBody(resp)
		if err != nil {
			return nil, err
		}
		var parsed gamesResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		all = append(all, parsed.Items...)

		next := ""
		if parsed.Links.Next != nil {
			next = parsed.Links.Next.Href
		}
		if next == "" {
			break
		}
		path = next
		params = nil
	}
	return all, nil
}

func (c *Client) InitGame(ctx context.Context, req InitGameRequest) (string, error) {
	if req.GameUUID == "" {
		return "", fmt.Errorf("game_uuid required")
	}
	if !req.Demo {
		if req.PlayerID == "" || req.PlayerName == "" || req.Currency == "" || req.SessionID == "" {
			return "", fmt.Errorf("player_id, player_name, currency, session_id required for non-demo")
		}
	}
	params := url.Values{}
	params.Set("game_uuid", req.GameUUID)
	if !req.Demo {
		params.Set("player_id", req.PlayerID)
		params.Set("player_name", req.PlayerName)
		params.Set("currency", req.Currency)
		params.Set("session_id", req.SessionID)
	}
	if req.Device != "" {
		params.Set("device", req.Device)
	}
	if req.ReturnURL != "" {
		params.Set("return_url", req.ReturnURL)
	}
	if req.Language != "" {
		params.Set("language", req.Language)
	}
	if req.Email != "" {
		params.Set("email", req.Email)
	}
	if req.LobbyData != "" {
		params.Set("lobby_data", req.LobbyData)
	}

	path := "/games/init"
	if req.Demo {
		path = "/games/init-demo"
	}
	resp, err := c.doRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return "", err
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return "", err
	}
	var parsed initGameResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.URL == "" {
		return "", fmt.Errorf("empty launch url")
	}
	return parsed.URL, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, error) {
	fullURL := path
	if !strings.HasPrefix(path, "http") {
		fullURL = c.baseURL + path
	}
	u, err := url.Parse(fullURL)
	if err != nil {
		return nil, err
	}

	if method == http.MethodGet {
		if params != nil {
			u.RawQuery = params.Encode()
		}
	} else if method == http.MethodPost {
		// keep params for body
	} else {
		return nil, fmt.Errorf("unsupported method %s", method)
	}

	var body io.Reader
	if method == http.MethodPost {
		if params == nil {
			params = url.Values{}
		}
		body = strings.NewReader(params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	signParams := params
	if method == http.MethodGet {
		signParams = u.Query()
	}
	headers := c.signedHeaders(signParams)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseAPIError(resp.StatusCode, bodyBytes)
	}
	return resp, nil
}

func (c *Client) signedHeaders(params url.Values) map[string]string {
	headers := map[string]string{
		"X-Merchant-Id": c.merchantID,
		"X-Timestamp":   strconv.FormatInt(time.Now().Unix(), 10),
		"X-Nonce":       uuid.NewString(),
	}
	merged := map[string]string{}
	for key, values := range params {
		if len(values) > 0 {
			merged[key] = values[0]
		}
	}
	for key, value := range headers {
		merged[key] = value
	}
	headers["X-Sign"] = auth.BuildSign(merged, c.merchantKey)
	return headers
}

type apiError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Code    int    `json:"code"`
	Status  int    `json:"status"`
}

func parseAPIError(status int, body []byte) error {
	var parsed apiError
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		return fmt.Errorf("slotegrator api error %d: %s", status, parsed.Message)
	}
	return fmt.Errorf("slotegrator api error %d: %s", status, strings.TrimSpace(string(body)))
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
