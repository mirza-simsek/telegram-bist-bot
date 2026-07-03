package analysis

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"telegram-bist-bot/internal/market"
)

func TestIntradayScanTradingViewIntegration(t *testing.T) {
	if os.Getenv("TV_SCAN_INTEGRATION") != "1" {
		t.Skip("set TV_SCAN_INTEGRATION=1 to run a full TradingView intraday scan")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	symbols, invalid, err := LoadSymbols(filepath.Join(root, "data", "bist_tum_hisseler.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(invalid) > 0 {
		t.Fatalf("invalid symbols: %v", invalid)
	}

	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}
	client := market.NewTradingViewChartClient(12*time.Second, 4)
	snapshotClient := market.NewTradingViewClient(12 * time.Second)
	scanner := NewScanner(client, snapshotClient, loc, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	report, err := scanner.ScanIntraday(ctx, Universe{Key: "tum", Label: "BIST Tum", Symbols: symbols}, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("data=%d/%d analyzed=%d results=%d duration=%s source=%s", report.DataSymbols, report.TotalSymbols, report.AnalyzedSymbols, len(report.Results), report.Duration(), report.Source)
	if len(report.ErrorSamples) > 0 {
		t.Logf("error samples: %v", report.ErrorSamples)
	}
	if report.DataSymbols < 400 {
		t.Fatalf("data coverage too low: got %d/%d", report.DataSymbols, report.TotalSymbols)
	}
	if report.AnalyzedSymbols < 300 {
		t.Fatalf("analyzed coverage too low: got %d/%d", report.AnalyzedSymbols, report.TotalSymbols)
	}
}
