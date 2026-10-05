package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type PythonScanner struct {
	executable string
	scriptPath string
	location   *time.Location
	maxResults int
	batchSize  int
	workers    int
	yfThreads  bool
}

func NewPythonScanner(executable string, scriptPath string, location *time.Location, maxResults int, batchSize int, workers int, yfThreads bool) *PythonScanner {
	return &PythonScanner{
		executable: executable,
		scriptPath: scriptPath,
		location:   location,
		maxResults: maxResults,
		batchSize:  batchSize,
		workers:    workers,
		yfThreads:  yfThreads,
	}
}

func (p *PythonScanner) ScanDaily(ctx context.Context, universe Universe, minScore int) (*Report, error) {
	return p.scan(ctx, ModeDaily, universe, minScore)
}

func (p *PythonScanner) ScanIntraday(ctx context.Context, universe Universe, minScore int) (*Report, error) {
	return p.scan(ctx, ModeIntraday, universe, minScore)
}

func (p *PythonScanner) AnalyzeSymbol(ctx context.Context, rawSymbol string) (*SymbolAnalysis, error) {
	symbol := normalizeSymbol(rawSymbol)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is empty")
	}
	scriptPath, err := filepath.Abs(p.scriptPath)
	if err != nil {
		return nil, fmt.Errorf("resolve python scanner script: %w", err)
	}
	args := []string{scriptPath, "--mode", "card", "--symbol", symbol, "--timezone", p.location.String()}
	cmd := exec.CommandContext(ctx, p.executable, args...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("python symbol analysis failed: %w: %s", err, truncateScannerText(stderr.String(), 800))
	}
	card, err := parsePythonCard(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse python symbol output: %w: %s", err, truncateScannerText(stdout.String(), 800))
	}
	if len(card.Sections) == 0 {
		return nil, fmt.Errorf("not enough completed market data for %s", symbol)
	}
	return card, nil
}

func (p *PythonScanner) scan(ctx context.Context, mode Mode, universe Universe, minScore int) (*Report, error) {
	if len(universe.Symbols) == 0 {
		return nil, fmt.Errorf("%s universe has no symbols", universe.Label)
	}
	if strings.TrimSpace(p.executable) == "" {
		return nil, fmt.Errorf("python executable is empty")
	}
	if strings.TrimSpace(p.scriptPath) == "" {
		return nil, fmt.Errorf("python scanner script is empty")
	}

	scriptPath, err := filepath.Abs(p.scriptPath)
	if err != nil {
		return nil, fmt.Errorf("resolve python scanner script: %w", err)
	}
	symbolsFile, err := filepath.Abs(universe.SymbolsFile)
	if err != nil {
		return nil, fmt.Errorf("resolve symbols file: %w", err)
	}

	args := []string{
		scriptPath,
		"--mode", string(mode),
		"--symbols-file", symbolsFile,
		"--universe-key", universe.Key,
		"--universe-name", universe.Label,
		"--min-score", strconv.Itoa(minScore),
		"--max-results", strconv.Itoa(p.maxResults),
		"--timezone", p.location.String(),
		"--batch-size", strconv.Itoa(p.batchSize),
		"--workers", strconv.Itoa(p.workers),
	}
	if p.yfThreads {
		args = append(args, "--yf-threads")
	}

	cmd := exec.CommandContext(ctx, p.executable, args...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("python scanner failed: %w: %s", err, truncateScannerText(stderr.String(), 800))
	}

	report, err := parsePythonReport(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse python scanner output: %w: %s", err, truncateScannerText(stdout.String(), 800))
	}
	report.Mode = mode
	report.UniverseKey = universe.Key
	report.UniverseName = universe.Label
	report.TotalSymbols = len(universe.Symbols)
	report.MinScore = minScore
	report.MaxResults = p.maxResults
	if report.StartedAt.IsZero() {
		report.StartedAt = time.Now().In(p.location)
	}
	if report.FinishedAt.IsZero() {
		report.FinishedAt = time.Now().In(p.location)
	}
	if report.DataSymbols == 0 {
		return report, fmt.Errorf("no %s market data could be downloaded", mode)
	}
	if report.DataSymbols < minimumPythonDataSymbols(report.TotalSymbols) {
		return report, fmt.Errorf("python scanner returned too little market data: %d/%d symbols", report.DataSymbols, report.TotalSymbols)
	}
	if report.AnalyzedSymbols < minimumPythonDataSymbols(report.TotalSymbols) {
		return report, fmt.Errorf("python scanner returned too little completed-bar analysis: %d/%d symbols", report.AnalyzedSymbols, report.TotalSymbols)
	}
	return report, nil
}

func minimumPythonDataSymbols(total int) int {
	if total <= 0 {
		return 1
	}
	minimum := total / 2
	if minimum < 20 {
		minimum = 20
	}
	if minimum > total {
		return total
	}
	return minimum
}

type pythonReport struct {
	Mode            Mode           `json:"mode"`
	UniverseKey     string         `json:"universe_key"`
	UniverseName    string         `json:"universe_name"`
	StartedAt       string         `json:"started_at"`
	FinishedAt      string         `json:"finished_at"`
	TotalSymbols    int            `json:"total_symbols"`
	DataSymbols     int            `json:"data_symbols"`
	AnalyzedSymbols int            `json:"analyzed_symbols"`
	FailedSymbols   int            `json:"failed_symbols"`
	MinScore        int            `json:"min_score"`
	MaxResults      int            `json:"max_results"`
	Results         []pythonSignal `json:"results"`
	Source          string         `json:"source"`
	IntervalSummary string         `json:"interval_summary"`
	FilterSummary   string         `json:"filter_summary"`
	ErrorSamples    []string       `json:"error_samples"`
}

type pythonSignal struct {
	Symbol     string   `json:"symbol"`
	Score      int      `json:"score"`
	TLScore    int      `json:"tl_score"`
	TLStatus   string   `json:"tl_status"`
	USDScore   int      `json:"usd_score"`
	USDStatus  string   `json:"usd_status"`
	USDPrice   float64  `json:"usd_price"`
	USDQuality string   `json:"usd_data_quality"`
	Price      float64  `json:"price"`
	StopLoss   float64  `json:"stop_loss"`
	RSI        float64  `json:"rsi"`
	RSI5M      float64  `json:"rsi_5m"`
	RSI15M     float64  `json:"rsi_15m"`
	VolumeX    float64  `json:"volume_x"`
	VolumeX5M  float64  `json:"volume_x_5m"`
	VolumeX15M float64  `json:"volume_x_15m"`
	Approval   string   `json:"approval"`
	VWAP5M     string   `json:"vwap_5m"`
	POC5M      string   `json:"poc_5m"`
	VWAP15M    string   `json:"vwap_15m"`
	POC15M     string   `json:"poc_15m"`
	Details    []string `json:"details"`
	TLDetails  []string `json:"tl_details"`
	USDDetails []string `json:"usd_details"`
}

func parsePythonReport(payload []byte) (*Report, error) {
	var raw pythonReport
	if err := json.Unmarshal(bytes.TrimSpace(payload), &raw); err != nil {
		return nil, err
	}
	startedAt, err := parsePythonTime(raw.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("started_at: %w", err)
	}
	finishedAt, err := parsePythonTime(raw.FinishedAt)
	if err != nil {
		return nil, fmt.Errorf("finished_at: %w", err)
	}
	report := &Report{
		Mode:            raw.Mode,
		UniverseKey:     raw.UniverseKey,
		UniverseName:    raw.UniverseName,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		TotalSymbols:    raw.TotalSymbols,
		DataSymbols:     raw.DataSymbols,
		AnalyzedSymbols: raw.AnalyzedSymbols,
		FailedSymbols:   raw.FailedSymbols,
		MinScore:        raw.MinScore,
		MaxResults:      raw.MaxResults,
		Source:          raw.Source,
		IntervalSummary: raw.IntervalSummary,
		FilterSummary:   raw.FilterSummary,
		ErrorSamples:    raw.ErrorSamples,
	}
	for _, item := range raw.Results {
		report.Results = append(report.Results, Signal{
			Symbol:     item.Symbol,
			Score:      item.Score,
			TLScore:    item.TLScore,
			TLStatus:   item.TLStatus,
			USDScore:   item.USDScore,
			USDStatus:  item.USDStatus,
			USDPrice:   item.USDPrice,
			USDQuality: item.USDQuality,
			Price:      item.Price,
			StopLoss:   item.StopLoss,
			RSI:        item.RSI,
			RSI5M:      item.RSI5M,
			RSI15M:     item.RSI15M,
			VolumeX:    item.VolumeX,
			VolumeX5M:  item.VolumeX5M,
			VolumeX15M: item.VolumeX15M,
			Approval:   item.Approval,
			VWAP5M:     item.VWAP5M,
			POC5M:      item.POC5M,
			VWAP15M:    item.VWAP15M,
			POC15M:     item.POC15M,
			Details:    item.Details,
			TLDetails:  item.TLDetails,
			USDDetails: item.USDDetails,
		})
	}
	return report, nil
}

type pythonCard struct {
	Symbol     string             `json:"symbol"`
	FinishedAt string             `json:"finished_at"`
	Source     string             `json:"source"`
	IntradayTL *pythonScoreResult `json:"intraday_tl"`
	DailyTL    *pythonScoreResult `json:"daily_tl"`
	DailyUSD   *pythonScoreResult `json:"daily_usd"`
	FX         pythonFXMetadata   `json:"fx"`
}

type pythonFXMetadata struct {
	USDQuality string  `json:"usd_data_quality"`
	USDTRY     float64 `json:"usdtry"`
	USDTRYTime string  `json:"usdtry_time"`
}

type pythonScoreResult struct {
	Score    int           `json:"score"`
	Status   string        `json:"status"`
	Price    float64       `json:"price"`
	Details  []string      `json:"details"`
	Warnings []string      `json:"warnings"`
	Metrics  pythonMetrics `json:"metrics"`
}

type pythonMetrics struct {
	Currency       string  `json:"currency"`
	RSI            float64 `json:"rsi"`
	RSI5M          float64 `json:"rsi_5m"`
	RSI15M         float64 `json:"rsi_15m"`
	VolumeX        float64 `json:"volume_x"`
	VolumeX5M      float64 `json:"volume_x_5m"`
	VolumeX15M     float64 `json:"volume_x_15m"`
	CMF            float64 `json:"cmf"`
	CMF5M          float64 `json:"cmf_5m"`
	CMF15M         float64 `json:"cmf_15m"`
	Support        float64 `json:"support"`
	Resistance     float64 `json:"resistance"`
	SMA20          float64 `json:"sma20"`
	SMA50          float64 `json:"sma50"`
	SMA200         float64 `json:"sma200"`
	RangeTrend     int     `json:"range_trend"`
	RangeTrend15   int     `json:"range_trend_15m"`
	BarClosedAt    string  `json:"bar_closed_at"`
	BarClosedAt5M  string  `json:"bar_closed_at_5m"`
	BarClosedAt15M string  `json:"bar_closed_at_15m"`
}

func parsePythonCard(payload []byte) (*SymbolAnalysis, error) {
	var raw pythonCard
	if err := json.Unmarshal(bytes.TrimSpace(payload), &raw); err != nil {
		return nil, err
	}
	finishedAt, err := parsePythonTime(raw.FinishedAt)
	if err != nil {
		return nil, fmt.Errorf("finished_at: %w", err)
	}
	card := &SymbolAnalysis{Symbol: raw.Symbol, Source: raw.Source, FinishedAt: finishedAt, USDTRY: raw.FX.USDTRY, USDTRYAt: raw.FX.USDTRYTime}
	appendSection := func(key, label, currency, quality string, result *pythonScoreResult) {
		if result == nil {
			card.MissingNotes = append(card.MissingNotes, label+" verisi yetersiz")
			return
		}
		metrics := result.Metrics
		trend := metrics.RangeTrend
		if key == "intraday_tl" {
			trend = metrics.RangeTrend15
		}
		card.Sections = append(card.Sections, TechnicalSection{
			Key: key, Label: label, Currency: currency, Score: result.Score, MaxScore: 10,
			Status: result.Status, Price: result.Price, RSI: metrics.RSI,
			RSI5M: metrics.RSI5M, RSI15M: metrics.RSI15M, VolumeX: metrics.VolumeX,
			VolumeX5M: metrics.VolumeX5M, VolumeX15M: metrics.VolumeX15M,
			CMF: metrics.CMF, CMF5M: metrics.CMF5M, CMF15M: metrics.CMF15M,
			Support: metrics.Support, Resistance: metrics.Resistance,
			SMA20: metrics.SMA20, SMA50: metrics.SMA50, SMA200: metrics.SMA200,
			RangeTrend: trend, DataQuality: quality, Details: result.Details, Warnings: result.Warnings,
			BarClosedAt: metrics.BarClosedAt, BarClosedAt5M: metrics.BarClosedAt5M, BarClosedAt15M: metrics.BarClosedAt15M,
		})
	}
	appendSection("intraday_tl", "Gun ici TL (5dk + 15dk)", "TL", "OHLCV", raw.IntradayTL)
	appendSection("daily_tl", "Gunluk TL", "TL", "OHLCV", raw.DailyTL)
	appendSection("daily_usd", "Gunluk USD", "USD", raw.FX.USDQuality, raw.DailyUSD)
	if raw.DailyTL != nil {
		card.Price = raw.DailyTL.Price
		card.Verdict = "Gunluk TL: " + raw.DailyTL.Status
	}
	if raw.DailyUSD != nil {
		card.Verdict += " | Gunluk USD: " + raw.DailyUSD.Status
	}
	card.VerdictNote = "TL ve USD puanlari ayri yorumlanir; ortak veya agirlikli skor uretilmez."
	return card, nil
}

func parsePythonTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02T15:04:05-07:00", raw)
}

func truncateScannerText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
