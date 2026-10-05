package app

import (
	"strings"
	"testing"
	"time"

	"telegram-bist-bot/internal/analysis"
)

func TestFormatIntradayReportIncludesPythonScannerDetails(t *testing.T) {
	report := &analysis.Report{
		Mode:         analysis.ModeIntraday,
		UniverseName: "BIST Tum",
		FinishedAt:   time.Date(2026, 6, 24, 23, 37, 0, 0, time.UTC),
		Results: []analysis.Signal{
			{
				Symbol:     "HLGYO",
				Price:      6.52,
				Score:      8,
				RSI5M:      53.6,
				RSI15M:     61.3,
				VolumeX5M:  2.1,
				VolumeX15M: 3.7,
				VWAP5M:     "Ust",
				POC5M:      "Ust",
				VWAP15M:    "Ust",
				POC15M:     "Ust",
				Details:    []string{"1h boga trendi", "1h OBV", "1h VAH kirilimi", "hacim sicramasi"},
			},
		},
	}

	text := formatReport(report, "Gun Ici Guclu Sinyal Taramasi")
	required := []string{
		"HLGYO",
		"TL <code>8/10",
		"5dk VWAP Ust",
		"15dk VWAP Ust",
		"RSI <code>5d 53.6</code>",
		"Hacim <code>5d x2.1</code>",
		"5d trend",
		"5d OBV",
		"5d VAH kirilimi",
		"Hacim sicramasi",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted report missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Esigi gecen aday yok") {
		t.Fatalf("formatted report incorrectly rendered empty-result text:\n%s", text)
	}
}

func TestFormatDailyReportIncludesScannerDetails(t *testing.T) {
	report := &analysis.Report{
		Mode:         analysis.ModeDaily,
		UniverseName: "BIST Tum",
		FinishedAt:   time.Date(2026, 6, 24, 18, 20, 0, 0, time.UTC),
		Results: []analysis.Signal{
			{
				Symbol:   "AKBNK",
				Price:    81.30,
				Score:    6,
				RSI:      56.9,
				VolumeX:  1.6,
				Approval: "GUVENLI",
				Details:  []string{"boga", "OBV kirilim", "ideal RSI"},
			},
		},
	}

	text := formatReport(report, "Gunluk Alim Radari")
	required := []string{
		"AKBNK",
		"RSI <code>56.9</code>",
		"Hacim <code>x1.6</code>",
		"Onay <code>Guvenli</code>",
		"SMA200 ustu",
		"OBV kirilimi",
		"RSI ideal",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted report missing %q:\n%s", want, text)
		}
	}
}

func TestFormatIntradayReportWithTenCandidatesStaysTelegramSafe(t *testing.T) {
	signals := []analysis.Signal{
		{Symbol: "HLGYO", Price: 6.52, Score: 8, RSI5M: 53.6, RSI15M: 61.3, VolumeX5M: 2.1, VolumeX15M: 3.7, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h OBV", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "MRSHL", Price: 1654.00, Score: 7, RSI5M: 54.3, RSI15M: 49.7, VolumeX5M: 0.5, VolumeX15M: 2.0, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "GOZDE", Price: 23.86, Score: 7, RSI5M: 61.2, RSI15M: 71.3, VolumeX5M: 2.7, VolumeX15M: 4.1, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "DEVA", Price: 73.00, Score: 7, RSI5M: 63.2, RSI15M: 65.8, VolumeX5M: 1.3, VolumeX15M: 3.4, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "GEREL", Price: 43.58, Score: 7, RSI5M: 66.9, RSI15M: 58.8, VolumeX5M: 0.7, VolumeX15M: 1.7, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "SKBNK", Price: 16.89, Score: 7, RSI5M: 75.0, RSI15M: 72.9, VolumeX5M: 1.5, VolumeX15M: 2.2, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "MARKA", Price: 93.45, Score: 7, RSI5M: 78.9, RSI15M: 70.9, VolumeX5M: 1.1, VolumeX15M: 4.0, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "AKBNK", Price: 81.30, Score: 6, RSI5M: 56.9, RSI15M: 63.3, VolumeX5M: 1.2, VolumeX15M: 1.6, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h VAH kirilimi", "hacim sicramasi"}},
		{Symbol: "IDGYO", Price: 4.83, Score: 6, RSI5M: 60.8, RSI15M: 56.4, VolumeX5M: 2.2, VolumeX15M: 3.4, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "hacim sicramasi"}},
		{Symbol: "DGGYO", Price: 47.40, Score: 6, RSI5M: 62.3, RSI15M: 50.8, VolumeX5M: 0.5, VolumeX15M: 1.0, VWAP5M: "Ust", POC5M: "Ust", VWAP15M: "Ust", POC15M: "Ust", Details: []string{"1h boga trendi", "1h VAH kirilimi"}},
	}
	report := &analysis.Report{
		Mode:         analysis.ModeIntraday,
		UniverseName: "BIST Tum",
		FinishedAt:   time.Date(2026, 6, 24, 23, 37, 0, 0, time.UTC),
		Results:      signals,
	}

	text := formatReport(report, "Gun Ici Guclu Sinyal Taramasi")
	if len([]rune(text)) > 4096 {
		t.Fatalf("formatted report too long for Telegram: %d chars\n%s", len([]rune(text)), text)
	}
	for _, signal := range signals {
		if !strings.Contains(text, signal.Symbol) {
			t.Fatalf("formatted report missing %s:\n%s", signal.Symbol, text)
		}
	}
}
