package analysis

import (
	"testing"
	"time"

	"telegram-bist-bot/internal/market"
)

func TestAnalyzeSymbolSnapshotBuildsMultiTimeframeCard(t *testing.T) {
	values := map[string]float64{
		"close": 110,
	}
	for _, suffix := range []string{"|15", "|60", ""} {
		values["EMA9"+suffix] = 108
		values["EMA20"+suffix] = 105
		values["VWAP"+suffix] = 107
		values["RSI"+suffix] = 58
		values["Recommend.All"+suffix] = 0.35
		values["relative_volume_10d_calc"+suffix] = 1.6
		values["SMA200"+suffix] = 95
	}

	card, ok := analyzeSymbolSnapshot("ALARK", market.Snapshot{Symbol: "ALARK", Values: values}, time.Now())
	if !ok {
		t.Fatal("expected symbol analysis")
	}
	if card.Symbol != "ALARK" {
		t.Fatalf("symbol = %q, want ALARK", card.Symbol)
	}
	if len(card.Timeframes) != 3 {
		t.Fatalf("timeframes = %d, want 3", len(card.Timeframes))
	}
	if card.Verdict != "Alim radarinda" {
		t.Fatalf("verdict = %q, want Alim radarinda", card.Verdict)
	}
	for _, frame := range card.Timeframes {
		if frame.Bias != "Pozitif" {
			t.Fatalf("%s bias = %q, want Pozitif", frame.Key, frame.Bias)
		}
		if frame.Score == 0 || frame.MaxScore == 0 {
			t.Fatalf("%s score = %d/%d, want positive score", frame.Key, frame.Score, frame.MaxScore)
		}
	}
}

func TestAnalyzeSymbolSnapshotFiltersInvalidLevelData(t *testing.T) {
	values := map[string]float64{
		"close": 100,
	}
	for _, suffix := range []string{"|15", "|60", ""} {
		values["EMA9"+suffix] = 98
		values["EMA20"+suffix] = 96
		values["VWAP"+suffix] = 100_000
		values["RSI"+suffix] = 55
		values["Recommend.All"+suffix] = 0.2
		values["relative_volume_10d_calc"+suffix] = 1
		values["SMA200"+suffix] = 90
	}

	card, ok := analyzeSymbolSnapshot("TEST", market.Snapshot{Symbol: "TEST", Values: values}, time.Now())
	if !ok {
		t.Fatal("expected symbol analysis with filtered bad VWAP")
	}
	if len(card.DataWarnings) == 0 {
		t.Fatal("expected data warning for invalid VWAP")
	}
	for _, frame := range card.Timeframes {
		if frame.VWAP != 0 {
			t.Fatalf("%s VWAP = %.2f, want filtered zero", frame.Key, frame.VWAP)
		}
	}
}
