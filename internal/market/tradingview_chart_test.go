package market

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestTradingViewChartClientIntegration(t *testing.T) {
	if os.Getenv("TV_CHART_INTEGRATION") != "1" {
		t.Skip("set TV_CHART_INTEGRATION=1 to call TradingView chart websocket")
	}

	client := NewTradingViewChartClient(12*time.Second, 2)
	candles, err := client.Fetch(context.Background(), "THYAO", "1mo", "15m")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(candles) < 200 {
		t.Fatalf("got %d candles, want at least 200", len(candles))
	}
	last := candles[len(candles)-1]
	if last.Close <= 0 || last.Volume < 0 {
		t.Fatalf("invalid last candle: %+v", last)
	}
}

func TestTradingViewChartClientFetchManyIntegration(t *testing.T) {
	if os.Getenv("TV_CHART_INTEGRATION") != "1" {
		t.Skip("set TV_CHART_INTEGRATION=1 to call TradingView chart websocket")
	}

	symbols := []string{"DMSAS", "CELHA", "DGGYO", "GEREL", "EGPRO", "TDGYO", "GLRYH", "KTSKR", "BINHO", "GOODY"}
	client := NewTradingViewChartClient(12*time.Second, 2)
	result := client.FetchMany(context.Background(), symbols, "1mo", "15m")
	if len(result.Data) < len(symbols) {
		t.Fatalf("got %d/%d symbols, errors=%v", len(result.Data), len(symbols), result.Errors)
	}
	for _, symbol := range symbols {
		candles := result.Data[symbol]
		if len(candles) < 200 {
			t.Fatalf("%s got %d candles, want at least 200", symbol, len(candles))
		}
	}
}
