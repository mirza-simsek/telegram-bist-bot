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
	TLScore    int
	TLStatus   string
	USDScore   int
	USDStatus  string
	USDPrice   float64
	USDQuality string
	Price      float64
	StopLoss   float64
	RSI        float64
	RSI5M      float64
	RSI15M     float64
	VolumeX    float64
	VolumeX5M  float64
	VolumeX15M float64
	Approval   string
	VWAP5M     string
	POC5M      string
	VWAP15M    string
	POC15M     string
	Details    []string
	TLDetails  []string
	USDDetails []string
}

type SymbolAnalysis struct {
	Symbol       string
	Price        float64
	Score        int
	MaxScore     int
	Verdict      string
	VerdictNote  string
	Source       string
	USDTRY       float64
	USDTRYAt     string
	FinishedAt   time.Time
	Timeframes   []TimeframeAnalysis
	Sections     []TechnicalSection
	MissingNotes []string
	DataWarnings []string
}

// TechnicalSection is one independent view in the shared-engine symbol card.
// Intraday is TL-only; daily TL and daily USD are never combined into one score.
type TechnicalSection struct {
	Key            string
	Label          string
	Currency       string
	Score          int
	MaxScore       int
	Status         string
	Price          float64
	RSI            float64
	RSI5M          float64
	RSI15M         float64
	VolumeX        float64
	VolumeX5M      float64
	VolumeX15M     float64
	CMF            float64
	CMF5M          float64
	CMF15M         float64
	Support        float64
	Resistance     float64
	SMA20          float64
	SMA50          float64
	SMA200         float64
	RangeTrend     int
	DataQuality    string
	BarClosedAt    string
	BarClosedAt5M  string
	BarClosedAt15M string
	Details        []string
	Warnings       []string
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
