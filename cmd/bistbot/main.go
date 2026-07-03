package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"telegram-bist-bot/internal/analysis"
	"telegram-bist-bot/internal/app"
	"telegram-bist-bot/internal/config"
	"telegram-bist-bot/internal/market"
	"telegram-bist-bot/internal/telegram"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	allSymbols, invalidAllSymbols, err := analysis.LoadSymbols(cfg.AllSymbolsFile)
	if err != nil {
		log.Fatalf("all symbols error: %v", err)
	}
	if len(invalidAllSymbols) > 0 {
		log.Printf("ignored %d invalid symbols from %s", len(invalidAllSymbols), cfg.AllSymbolsFile)
	}

	bist100Symbols, invalidBIST100Symbols, err := analysis.LoadSymbols(cfg.BIST100SymbolsFile)
	if err != nil {
		log.Fatalf("bist100 symbols error: %v", err)
	}
	if len(invalidBIST100Symbols) > 0 {
		log.Printf("ignored %d invalid symbols from %s", len(invalidBIST100Symbols), cfg.BIST100SymbolsFile)
	}

	universes := []analysis.Universe{
		{Key: "tum", Label: "BIST Tum", SymbolsFile: cfg.AllSymbolsFile, Symbols: allSymbols},
		{Key: "bist100", Label: "BIST 100", SymbolsFile: cfg.BIST100SymbolsFile, Symbols: bist100Symbols},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	telegramClient := telegram.NewClient(cfg.TelegramToken, cfg.HTTPTimeout)
	if err := telegramClient.GetMe(ctx); err != nil {
		log.Fatalf("telegram token check failed: %v", err)
	}
	if err := telegramClient.SetMyCommands(ctx, []telegram.BotCommand{
		{Command: "gunici100", Description: "BIST 100 gun ici tarama"},
		{Command: "gunicitum", Description: "BIST Tum gun ici tarama"},
		{Command: "gunluk100", Description: "BIST 100 gunluk radar"},
		{Command: "gunluktum", Description: "BIST Tum gunluk radar"},
		{Command: "durum", Description: "Son tarama durumu"},
		{Command: "ayarlar", Description: "Aktif esikler ve zamanlama"},
		{Command: "reset", Description: "Aktif islemi durdur"},
		{Command: "help", Description: "Yardim ve komut listesi"},
	}); err != nil {
		log.Printf("telegram setMyCommands failed: %v", err)
	}

	chartClient := market.NewTradingViewChartClient(cfg.HTTPTimeout, cfg.TradingViewChartConcurrency)
	tradingViewClient := market.NewTradingViewClient(cfg.HTTPTimeout)
	scanner := analysis.NewScanner(chartClient, tradingViewClient, cfg.MarketTimezone, cfg.MaxResults)
	if cfg.PythonScannerEnabled {
		pythonScanner := analysis.NewPythonScanner(
			cfg.PythonExecutable,
			cfg.PythonScannerScript,
			cfg.MarketTimezone,
			cfg.MaxResults,
			cfg.PythonScannerBatchSize,
			cfg.PythonScannerWorkers,
			cfg.PythonScannerYFThreads,
		)
		scanner.SetPythonScanner(pythonScanner)
		log.Printf("python scanner enabled executable=%s script=%s batch_size=%d workers=%d yf_threads=%t", cfg.PythonExecutable, cfg.PythonScannerScript, cfg.PythonScannerBatchSize, cfg.PythonScannerWorkers, cfg.PythonScannerYFThreads)
	}
	botApp := app.New(cfg, telegramClient, scanner, universes)

	log.Printf("bist bot started with %d all symbols, %d bist100 symbols, timezone=%s", len(allSymbols), len(bist100Symbols), cfg.MarketTimezoneRaw)
	err = botApp.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("bot stopped: %v", err)
	}
	log.Println("bot stopped")
}
