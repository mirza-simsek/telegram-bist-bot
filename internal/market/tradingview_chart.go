package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

const tradingViewSocketURL = "wss://data.tradingview.com/socket.io/websocket?from=chart%2F"
const tradingViewChartBatchSize = 5

type TradingViewChartClient struct {
	timeout     time.Duration
	concurrency int
}

func NewTradingViewChartClient(timeout time.Duration, concurrency int) *TradingViewChartClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if concurrency < 1 {
		concurrency = 4
	}
	return &TradingViewChartClient{
		timeout:     timeout,
		concurrency: concurrency,
	}
}

func (c *TradingViewChartClient) FetchMany(ctx context.Context, symbols []string, rangeParam string, interval string) BatchResult {
	result := BatchResult{
		Data:   make(map[string][]Candle),
		Errors: make(map[string]error),
	}
	if len(symbols) == 0 {
		return result
	}

	jobs := make(chan []string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range jobs {
				batchResult := c.fetchBatchWithRetry(ctx, batch, rangeParam, interval)
				mu.Lock()
				for symbol, candles := range batchResult.Data {
					result.Data[symbol] = candles
				}
				for symbol, err := range batchResult.Errors {
					if _, ok := result.Data[symbol]; !ok {
						result.Errors[symbol] = err
					}
				}
				mu.Unlock()
			}
		}()
	}

sendLoop:
	for start := 0; start < len(symbols); start += tradingViewChartBatchSize {
		end := start + tradingViewChartBatchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- symbols[start:end]:
		}
	}
	close(jobs)
	wg.Wait()

	if ctx.Err() != nil {
		mu.Lock()
		for _, symbol := range symbols {
			key := normalizeRawSymbol(symbol)
			if _, ok := result.Data[key]; !ok {
				result.Errors[key] = ctx.Err()
			}
		}
		mu.Unlock()
	}
	return result
}

func (c *TradingViewChartClient) Fetch(ctx context.Context, symbol string, rangeParam string, interval string) ([]Candle, error) {
	result := c.fetchBatchWithRetry(ctx, []string{symbol}, rangeParam, interval)
	key := normalizeRawSymbol(symbol)
	if candles, ok := result.Data[key]; ok {
		return candles, nil
	}
	if err, ok := result.Errors[key]; ok {
		return nil, err
	}
	return nil, fmt.Errorf("tradingview returned no candles for %s", symbol)
}

func (c *TradingViewChartClient) fetchBatchWithRetry(ctx context.Context, symbols []string, rangeParam string, interval string) BatchResult {
	var result BatchResult
	for attempt := 1; attempt <= 4; attempt++ {
		result = c.fetchBatch(ctx, symbols, rangeParam, interval)
		if !batchRetryable(result) || attempt == 4 {
			return result
		}
		wait := time.Duration(attempt*attempt)*2*time.Second + time.Duration(rand.Intn(750))*time.Millisecond
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result
		case <-timer.C:
		}
	}
	return result
}

func (c *TradingViewChartClient) fetchBatch(ctx context.Context, symbols []string, rangeParam string, interval string) BatchResult {
	result := BatchResult{
		Data:   make(map[string][]Candle),
		Errors: make(map[string]error),
	}
	if len(symbols) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, c.batchTimeout(len(symbols)))
	defer cancel()

	ws, _, err := websocket.Dial(ctx, tradingViewSocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":     []string{"https://www.tradingview.com"},
			"User-Agent": []string{"Mozilla/5.0 (compatible; telegram-bist-bot/1.0)"},
		},
	})
	if err != nil {
		for _, symbol := range symbols {
			result.Errors[normalizeRawSymbol(symbol)] = err
		}
		return result
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(32 << 20)

	tvInterval := tradingViewInterval(interval)
	barCount := tradingViewBarCount(rangeParam, interval)

	sessionSymbols := make(map[string]string, len(symbols))
	pending := make(map[string]struct{}, len(symbols))
	if err := writeTVMessage(ctx, ws, tvMessage{Method: "set_auth_token", Params: []any{"unauthorized_user_token"}}); err != nil {
		for _, symbol := range symbols {
			result.Errors[normalizeRawSymbol(symbol)] = err
		}
		return result
	}
	for _, symbol := range symbols {
		key := normalizeRawSymbol(symbol)
		if key == "" {
			continue
		}
		session := randomTVSession("cs")
		sessionSymbols[session] = key
		pending[session] = struct{}{}
		requests := []tvMessage{
			{Method: "chart_create_session", Params: []any{session, ""}},
			{Method: "switch_timezone", Params: []any{session, "Europe/Istanbul"}},
			{Method: "resolve_symbol", Params: []any{session, "symbol_1", fmt.Sprintf(`={"symbol":"%s","adjustment":"splits","session":"regular"}`, tradingViewSymbol(key))}},
			{Method: "create_series", Params: []any{session, "s1", "s1", "symbol_1", tvInterval, barCount}},
		}
		for _, request := range requests {
			if err := writeTVMessage(ctx, ws, request); err != nil {
				result.Errors[key] = err
				delete(pending, session)
				break
			}
		}
	}

	for len(pending) > 0 {
		_, payload, err := ws.Read(ctx)
		if err != nil {
			for session := range pending {
				if symbol := sessionSymbols[session]; symbol != "" {
					result.Errors[symbol] = err
				}
			}
			return result
		}
		raw := string(payload)
		if strings.HasPrefix(raw, "~h~") {
			_ = ws.Write(ctx, websocket.MessageText, payload)
			continue
		}
		for _, message := range parseTVMessages(raw) {
			switch message.Method {
			case "timescale_update":
				session := messageSession(message)
				parsed := parseTVTimescale(message)
				if len(parsed) > 0 {
					if symbol := sessionSymbols[session]; symbol != "" {
						result.Data[symbol] = parsed
						delete(result.Errors, symbol)
						delete(pending, session)
					}
				}
			case "series_completed":
				session := messageSession(message)
				if _, ok := pending[session]; ok {
					if symbol := sessionSymbols[session]; symbol != "" {
						result.Errors[symbol] = fmt.Errorf("tradingview returned no candles for %s", symbol)
					}
					delete(pending, session)
				}
			case "critical_error":
				session := messageSession(message)
				if symbol := sessionSymbols[session]; symbol != "" {
					result.Errors[symbol] = fmt.Errorf("tradingview critical error for %s: %s", symbol, string(message.RawParams))
				}
				delete(pending, session)
			}
		}
	}
	return result
}

func batchRetryable(result BatchResult) bool {
	if len(result.Errors) == 0 {
		return false
	}
	for _, err := range result.Errors {
		if err == nil {
			continue
		}
		text := err.Error()
		if strings.Contains(text, "429") ||
			strings.Contains(text, "context deadline exceeded") ||
			strings.Contains(text, "failed to WebSocket dial") ||
			strings.Contains(text, "failed to get reader") {
			return true
		}
	}
	return false
}

type tvMessage struct {
	Method    string          `json:"m"`
	Params    []any           `json:"p"`
	RawParams json.RawMessage `json:"-"`
}

func writeTVMessage(ctx context.Context, ws *websocket.Conn, message tvMessage) error {
	payload, err := json.Marshal(map[string]any{
		"m": message.Method,
		"p": message.Params,
	})
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("~m~%d~m~%s", len(payload), payload)
	return ws.Write(ctx, websocket.MessageText, []byte(frame))
}

func parseTVMessages(raw string) []tvMessage {
	messages := make([]tvMessage, 0, 4)
	for len(raw) > 0 {
		if strings.HasPrefix(raw, "~h~") {
			return messages
		}
		if !strings.HasPrefix(raw, "~m~") {
			idx := strings.Index(raw, "~m~")
			if idx < 0 {
				return messages
			}
			raw = raw[idx:]
		}
		raw = strings.TrimPrefix(raw, "~m~")
		idx := strings.Index(raw, "~m~")
		if idx < 0 {
			return messages
		}
		size, err := strconv.Atoi(raw[:idx])
		if err != nil {
			return messages
		}
		raw = raw[idx+3:]
		if len(raw) < size {
			return messages
		}
		payload := raw[:size]
		raw = raw[size:]

		var envelope struct {
			Method string          `json:"m"`
			Params json.RawMessage `json:"p"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil || envelope.Method == "" {
			continue
		}
		var params []any
		_ = json.Unmarshal(envelope.Params, &params)
		messages = append(messages, tvMessage{
			Method:    envelope.Method,
			Params:    params,
			RawParams: envelope.Params,
		})
	}
	return messages
}

func messageSession(message tvMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	session, _ := message.Params[0].(string)
	return session
}

func parseTVTimescale(message tvMessage) []Candle {
	if len(message.RawParams) == 0 {
		return nil
	}
	var payload []any
	if err := json.Unmarshal(message.RawParams, &payload); err != nil || len(payload) < 2 {
		return nil
	}
	seriesByName, ok := payload[1].(map[string]any)
	if !ok {
		return nil
	}
	series, ok := seriesByName["s1"].(map[string]any)
	if !ok {
		return nil
	}
	rawBars, ok := series["s"].([]any)
	if !ok {
		return nil
	}

	candles := make([]Candle, 0, len(rawBars))
	for _, rawBar := range rawBars {
		bar, ok := rawBar.(map[string]any)
		if !ok {
			continue
		}
		values, ok := bar["v"].([]any)
		if !ok || len(values) < 6 {
			continue
		}
		candle := Candle{
			Time:   time.Unix(int64(numberAt(values, 0)), 0).UTC(),
			Open:   numberAt(values, 1),
			High:   numberAt(values, 2),
			Low:    numberAt(values, 3),
			Close:  numberAt(values, 4),
			Volume: numberAt(values, 5),
		}
		if validCandle(candle) {
			candles = append(candles, candle)
		}
	}
	return candles
}

func numberAt(values []any, idx int) float64 {
	if idx >= len(values) {
		return 0
	}
	value, _ := values[idx].(float64)
	return value
}

func tradingViewSymbol(symbol string) string {
	symbol = normalizeRawSymbol(symbol)
	if strings.Contains(symbol, ":") {
		return symbol
	}
	return "BIST:" + symbol
}

func tradingViewInterval(interval string) string {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1d", "d":
		return "1D"
	case "1h", "60m", "60":
		return "60"
	case "15m", "15":
		return "15"
	default:
		return strings.TrimSpace(interval)
	}
}

func tradingViewBarCount(rangeParam string, interval string) int {
	rangeParam = strings.ToLower(strings.TrimSpace(rangeParam))
	interval = strings.ToLower(strings.TrimSpace(interval))
	switch interval {
	case "1d", "d":
		return 370
	case "1h", "60m", "60":
		return 600
	case "15m", "15":
		return 900
	}
	switch rangeParam {
	case "1y":
		return 370
	case "3mo":
		return 600
	case "1mo":
		return 900
	default:
		return 500
	}
}

func (c *TradingViewChartClient) batchTimeout(size int) time.Duration {
	timeout := c.timeout
	if size > 1 {
		timeout += time.Duration(size/10) * time.Second
	}
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	return timeout
}

func randomTVSession(prefix string) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("_")
	for i := 0; i < 12; i++ {
		b.WriteByte(alphabet[rand.Intn(len(alphabet))])
	}
	return b.String()
}
