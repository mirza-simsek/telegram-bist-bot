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
	Price      float64  `json:"price"`
	StopLoss   float64  `json:"stop_loss"`
	RSI        float64  `json:"rsi"`
	RSI1H      float64  `json:"rsi_1h"`
	RSI15M     float64  `json:"rsi_15m"`
	VolumeX    float64  `json:"volume_x"`
	VolumeX1H  float64  `json:"volume_x_1h"`
	VolumeX15M float64  `json:"volume_x_15m"`
	Approval   string   `json:"approval"`
	VWAP1H     string   `json:"vwap_1h"`
	POC1H      string   `json:"poc_1h"`
	VWAP15M    string   `json:"vwap_15m"`
	POC15M     string   `json:"poc_15m"`
	Details    []string `json:"details"`
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
			Price:      item.Price,
			StopLoss:   item.StopLoss,
			RSI:        item.RSI,
			RSI1H:      item.RSI1H,
			RSI15M:     item.RSI15M,
			VolumeX:    item.VolumeX,
			VolumeX1H:  item.VolumeX1H,
			VolumeX15M: item.VolumeX15M,
			Approval:   item.Approval,
			VWAP1H:     item.VWAP1H,
			POC1H:      item.POC1H,
			VWAP15M:    item.VWAP15M,
			POC15M:     item.POC15M,
			Details:    item.Details,
		})
	}
	return report, nil
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
