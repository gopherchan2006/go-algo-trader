package bybit

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	baseURL    = "https://api-testnet.bybit.com"
	recvWindow = "5000"
)

type Client struct {
	apiKey    string
	secretKey string
	http      *http.Client
}

func New(apiKey, secretKey string) *Client {
	return &Client{
		apiKey:    apiKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) GetCoinBalance(coin string) (float64, error) {
	query := "accountType=UNIFIED&coin=" + coin

	body, err := c.get("/v5/account/wallet-balance", query)
	if err != nil {
		return 0, err
	}

	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Coin []struct {
					Coin          string `json:"coin"`
					WalletBalance string `json:"walletBalance"`
				} `json:"coin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse balance: %w", err)
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit: %s", resp.RetMsg)
	}

	for _, account := range resp.Result.List {
		for _, c := range account.Coin {
			if c.Coin == coin {
				val, _ := strconv.ParseFloat(c.WalletBalance, 64)
				return val, nil
			}
		}
	}

	return 0, fmt.Errorf("coin %s not found in balance", coin)
}

func (c *Client) GetLastPrice(symbol string) (float64, error) {
	query := "category=spot&symbol=" + symbol

	body, err := c.get("/v5/market/tickers", query)
	if err != nil {
		return 0, err
	}

	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				LastPrice string `json:"lastPrice"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parse ticker: %w", err)
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit: %s", resp.RetMsg)
	}
	if len(resp.Result.List) == 0 {
		return 0, fmt.Errorf("no ticker data for %s", symbol)
	}

	price, err := strconv.ParseFloat(resp.Result.List[0].LastPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("parse price: %w", err)
	}
	return price, nil
}

func (c *Client) MarketBuy(symbol string, usdtAmount float64) (string, error) {
	return c.placeOrder(map[string]string{
		"category":   "spot",
		"symbol":     symbol,
		"side":       "Buy",
		"orderType":  "Market",
		"qty":        fmt.Sprintf("%.2f", usdtAmount),
		"marketUnit": "quoteCoin",
	})
}

func (c *Client) MarketSell(symbol string, qty float64) (string, error) {
	truncated := float64(int(qty*100000)) / 100000
	if truncated <= 0 {
		return "", fmt.Errorf("sell qty too small after truncation: %.8f", qty)
	}
	return c.placeOrder(map[string]string{
		"category":  "spot",
		"symbol":    symbol,
		"side":      "Sell",
		"orderType": "Market",
		"qty":       fmt.Sprintf("%.5f", truncated),
	})
}

func (c *Client) placeOrder(params map[string]string) (string, error) {
	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("marshal order: %w", err)
	}

	respBody, err := c.post("/v5/order/create", bodyBytes)
	if err != nil {
		return "", err
	}

	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			OrderID string `json:"orderId"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("parse order response: %w", err)
	}
	if resp.RetCode != 0 {
		return "", fmt.Errorf("bybit: %s", resp.RetMsg)
	}

	return resp.Result.OrderID, nil
}

func (c *Client) get(path, query string) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signPayload := ts + c.apiKey + recvWindow + query
	sig := c.sign(signPayload)

	req, err := http.NewRequest(http.MethodGet, baseURL+path+"?"+query, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	c.setHeaders(req, ts, sig)
	return c.do(req)
}

func (c *Client) post(path string, body []byte) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signPayload := ts + c.apiKey + recvWindow + string(body)
	sig := c.sign(signPayload)

	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req, ts, sig)
	return c.do(req)
}

func (c *Client) setHeaders(req *http.Request, ts, sig string) {
	req.Header.Set("X-BAPI-API-KEY", c.apiKey)
	req.Header.Set("X-BAPI-SIGN", sig)
	req.Header.Set("X-BAPI-SIGN-TYPE", "2")
	req.Header.Set("X-BAPI-TIMESTAMP", ts)
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}

	return data, nil
}

func (c *Client) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(c.secretKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
