package analysis

import (
	"context"
	"time"

	"telegram-bist-bot/internal/market"
)

type Mode string

const (
	ModeDaily    Mode = "gunluk"
	ModeIntraday Mode = "gunici"
)

type Universe struct {
	Key         string
	Label       string
	SymbolsFile string
	Symbols     []string
}

type MarketClient interface {
	FetchMany(ctx context.Context, symbols []string, rangeParam string, interval string) market.BatchResult
}

type SnapshotClient interface {
	FetchSnapshots(ctx context.Context, symbols []string, columns []string) market.SnapshotResult
}

type Signal struct {
	Symbol     string
	Score      int
	Price      float64
	StopLoss   float64
	RSI        float64
	RSI1H      float64
	RSI15M     float64
	VolumeX    float64
	VolumeX1H  float64
	VolumeX15M float64
	Approval   string
	VWAP1H     string
	POC1H      string
	VWAP15M    string
	POC15M     string
	Details    []string
}

type SymbolAnalysis struct {
	Symbol       string
	Price        float64
	Score        int
	MaxScore     int
	Verdict      string
	VerdictNote  string
	Source       string
	FinishedAt   time.Time
	Timeframes   []TimeframeAnalysis
	MissingNotes []string
	DataWarnings []string
}

type TimeframeAnalysis struct {
	Key          string
	Label        string
	Price        float64
	Score        int
	MaxScore     int
	Bias         string
	EMA9         float64
	EMA20        float64
	VWAP         float64
	RSI          float64
	SMA200       float64
	Recommend    float64
	VolumeX      float64
	Notes        []string
	DataWarnings []string
}

type Report struct {
	Mode            Mode
	UniverseKey     string
	UniverseName    string
	StartedAt       time.Time
	FinishedAt      time.Time
	TotalSymbols    int
	DataSymbols     int
	AnalyzedSymbols int
	FailedSymbols   int
	MinScore        int
	MaxResults      int
	Results         []Signal
	Source          string
	IntervalSummary string
	FilterSummary   string
	ErrorSamples    []string
}

func (r Report) Duration() time.Duration {
	if r.FinishedAt.IsZero() {
		return time.Since(r.StartedAt)
	}
	return r.FinishedAt.Sub(r.StartedAt)
}
