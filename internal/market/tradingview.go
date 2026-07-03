package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type SnapshotResult struct {
	Data   map[string]Snapshot
	Errors map[string]error
}

type Snapshot struct {
	Symbol string
	Values map[string]float64
}

type TradingViewClient struct {
	endpoint   string
	httpClient *http.Client
}

func NewTradingViewClient(timeout time.Duration) *TradingViewClient {
	return &TradingViewClient{
		endpoint: "https://scanner.tradingview.com/turkey/scan",
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *TradingViewClient) FetchSnapshots(ctx context.Context, symbols []string, columns []string) SnapshotResult {
	result := SnapshotResult{
		Data:   make(map[string]Snapshot),
		Errors: make(map[string]error),
	}
	if len(symbols) == 0 {
		return result
	}

	payload := map[string]any{
		"symbols": map[string]any{
			"tickers": tvSymbols(symbols),
			"query": map[string]any{
				"types": []string{},
			},
		},
		"columns": columns,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		result.Errors["request"] = err
		return result
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		result.Errors["request"] = err
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; telegram-bist-bot/1.0)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		result.Errors["request"] = err
		return result
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		result.Errors["response"] = err
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Errors["response"] = fmt.Errorf("tradingview http status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return result
	}

	var payloadOut tvScanResponse
	if err := json.Unmarshal(respBody, &payloadOut); err != nil {
		result.Errors["decode"] = err
		return result
	}

	seen := make(map[string]struct{})
	for _, item := range payloadOut.Data {
		symbol := strings.TrimPrefix(strings.ToUpper(item.Symbol), "BIST:")
		values := make(map[string]float64)
		for idx, raw := range item.Data {
			if idx >= len(columns) {
				break
			}
			if number, ok := raw.(float64); ok {
				values[columns[idx]] = number
			}
		}
		if symbol == "" {
			continue
		}
		seen[symbol] = struct{}{}
		result.Data[symbol] = Snapshot{
			Symbol: symbol,
			Values: values,
		}
	}

	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if _, ok := seen[symbol]; !ok {
			result.Errors[symbol] = fmt.Errorf("tradingview returned no data")
		}
	}
	return result
}

func tvSymbols(symbols []string) []string {
	result := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if strings.Contains(symbol, ":") {
			result = append(result, symbol)
			continue
		}
		if dot := strings.Index(symbol, "."); dot >= 0 {
			symbol = symbol[:dot]
		}
		result = append(result, "BIST:"+symbol)
	}
	return result
}

type tvScanResponse struct {
	TotalCount int `json:"totalCount"`
	Data       []struct {
		Symbol string `json:"s"`
		Data   []any  `json:"d"`
	} `json:"data"`
}
