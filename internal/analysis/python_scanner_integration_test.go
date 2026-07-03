package analysis

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPythonScannerIntradayIntegration(t *testing.T) {
	if os.Getenv("PYTHON_SCANNER_INTEGRATION") != "1" {
		t.Skip("set PYTHON_SCANNER_INTEGRATION=1 to run")
	}
	executable := os.Getenv("PYTHON_EXECUTABLE")
	if executable == "" {
		executable = "python3"
	}
	script := os.Getenv("PYTHON_SCANNER_SCRIPT")
	if script == "" {
		script = "../../scripts/bist_data_scrap_bridge.py"
	}
	symbolsFile := os.Getenv("ALL_SYMBOLS_FILE")
	if symbolsFile == "" {
		symbolsFile = "../../data/bist_tum_hisseler.txt"
	}

	symbols, _, err := LoadSymbols(symbolsFile)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	scanner := NewPythonScanner(executable, script, loc, 10, 50, 4, false)
	report, err := scanner.ScanIntraday(ctx, Universe{
		Key:         "tum",
		Label:       "BIST Tum",
		SymbolsFile: symbolsFile,
		Symbols:     symbols,
	}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.DataSymbols < 250 {
		t.Fatalf("expected broad data coverage, got %d/%d", report.DataSymbols, report.TotalSymbols)
	}
	if report.AnalyzedSymbols < 250 {
		t.Fatalf("expected broad analysis coverage, got %d/%d", report.AnalyzedSymbols, report.TotalSymbols)
	}
	if len(report.Results) == 0 {
		t.Fatalf("expected at least one candidate, got none; data=%d analyzed=%d", report.DataSymbols, report.AnalyzedSymbols)
	}
	t.Logf("data=%d/%d analyzed=%d results=%d first=%s", report.DataSymbols, report.TotalSymbols, report.AnalyzedSymbols, len(report.Results), report.Results[0].Symbol)
}
