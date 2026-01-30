package wallets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 3 * time.Second},
	}
}

type BalanceResponse struct {
	Balance string `json:"balance"`
}

type GameRequest struct {
	PlayerID      string `json:"playerId"`
	WalletID      string `json:"walletId,omitempty"`
	Amount        string `json:"amount,omitempty"`
	Currency      string `json:"currency"`
	TransactionID string `json:"transactionId"`
	RoundID       string `json:"roundId,omitempty"`
	SessionID     string `json:"sessionId,omitempty"`
	Type          string `json:"type,omitempty"`
}

type GameResponse struct {
	Balance       string `json:"balance"`
	TransactionID string `json:"transaction_id"`
}

type walletErrorResponse struct {
	Error walletErrorPayload `json:"error"`
}

type walletErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var ErrInsufficientFunds = errors.New("insufficient_funds")

func (c *Client) GetBalance(ctx context.Context, playerID, currency string) (BalanceResponse, error) {
	url := fmt.Sprintf("%s/internal/game/balance?playerId=%s&currency=%s", c.baseURL, playerID, currency)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return BalanceResponse{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return BalanceResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return BalanceResponse{}, parseWalletError(resp.StatusCode, raw)
	}
	var parsed BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return BalanceResponse{}, err
	}
	return parsed, nil
}

func (c *Client) PostAction(ctx context.Context, action string, reqBody GameRequest) (GameResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return GameResponse{}, err
	}
	url := fmt.Sprintf("%s/internal/game/%s", c.baseURL, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return GameResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return GameResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return GameResponse{}, parseWalletError(resp.StatusCode, raw)
	}
	var parsed GameResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return GameResponse{}, err
	}
	return parsed, nil
}

func parseWalletError(status int, raw []byte) error {
	var parsed walletErrorResponse
	if err := json.Unmarshal(raw, &parsed); err == nil {
		code := strings.ToLower(parsed.Error.Code)
		if code == "insufficient_funds" {
			return ErrInsufficientFunds
		}
		if parsed.Error.Code != "" || parsed.Error.Message != "" {
			return fmt.Errorf("wallets error %s: %s", parsed.Error.Code, parsed.Error.Message)
		}
	}
	return fmt.Errorf("wallets error %d: %s", status, strings.TrimSpace(string(raw)))
}
