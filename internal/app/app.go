package app

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"sync"
	"time"

	"telegram-bist-bot/internal/analysis"
	"telegram-bist-bot/internal/config"
	"telegram-bist-bot/internal/telegram"
)

type App struct {
	cfg           config.Config
	bot           *telegram.Client
	scanner       *analysis.Scanner
	scanLock      chan struct{}
	deduper       *deduper
	universes     map[string]analysis.Universe
	universeOrder []string
	knownSymbols  map[string]struct{}

	mu                 sync.Mutex
	runningMode        analysis.Mode
	runningUniverse    string
	runningStartedAt   time.Time
	runningCancel      context.CancelFunc
	lastReports        map[analysis.Mode]*analysis.Report
	lastErrors         map[analysis.Mode]string
	lastIntradayRunKey string
	lastDailyRunKey    string
}

func New(cfg config.Config, bot *telegram.Client, scanner *analysis.Scanner, universes []analysis.Universe) *App {
	universeMap := make(map[string]analysis.Universe, len(universes))
	universeOrder := make([]string, 0, len(universes))
	knownSymbols := make(map[string]struct{})
	for _, universe := range universes {
		key := normalizeUniverseKey(universe.Key)
		if key == "" {
			continue
		}
		universe.Key = key
		universeMap[key] = universe
		universeOrder = append(universeOrder, key)
		for _, symbol := range universe.Symbols {
			if normalized := normalizeCommandSymbol(symbol); normalized != "" {
				knownSymbols[normalized] = struct{}{}
			}
		}
	}
	return &App{
		cfg:           cfg,
		bot:           bot,
		scanner:       scanner,
		scanLock:      make(chan struct{}, 1),
		deduper:       newDeduper(cfg.AlertDedupWindow),
		universes:     universeMap,
		universeOrder: universeOrder,
		knownSymbols:  knownSymbols,
		lastReports:   make(map[analysis.Mode]*analysis.Report),
		lastErrors:    make(map[analysis.Mode]string),
	}
}

func (a *App) Run(ctx context.Context) error {
	go a.scheduleLoop(ctx)
	return a.bot.Poll(ctx, a.handleUpdate)
}

func (a *App) handleUpdate(ctx context.Context, update telegram.Update) {
	if update.CallbackQuery != nil {
		a.handleCallbackQuery(ctx, *update.CallbackQuery)
		return
	}
	if update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
		return
	}
	chatID := update.Message.Chat.ID
	if !a.cfg.IsChatAllowed(chatID) {
		log.Printf("ignored message from unauthorized chat_id=%d", chatID)
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	fields := strings.Fields(text)
	rawCommand := fields[0]
	command := normalizeCommandName(rawCommand)
	log.Printf("received command=%s normalized=%s chat_id=%d", rawCommand, command, chatID)
	a.handleCommand(ctx, chatID, rawCommand)
}

func (a *App) handleCallbackQuery(ctx context.Context, query telegram.CallbackQuery) {
	if query.Message == nil {
		_ = a.bot.AnswerCallbackQuery(ctx, query.ID, "")
		return
	}
	chatID := query.Message.Chat.ID
	if !a.cfg.IsChatAllowed(chatID) {
		log.Printf("ignored callback from unauthorized chat_id=%d", chatID)
		_ = a.bot.AnswerCallbackQuery(ctx, query.ID, "Bu sohbet yetkili degil.")
		return
	}
	command := normalizeCommandName(query.Data)
	log.Printf("received callback=%s normalized=%s chat_id=%d", query.Data, command, chatID)
	_ = a.bot.AnswerCallbackQuery(ctx, query.ID, "Tarama baslatiliyor.")
	a.handleCommand(ctx, chatID, query.Data)
}

func (a *App) handleCommand(ctx context.Context, chatID int64, rawCommand string) {
	command := normalizeCommandName(rawCommand)
	switch command {
	case "start", "help":
		_ = a.bot.SendMessage(ctx, chatID, a.formatHelp(chatID))
	case "gunluk100":
		go a.runManualScan(ctx, chatID, analysis.ModeDaily, a.universeByKey("bist100"))
	case "gunluktum":
		go a.runManualScan(ctx, chatID, analysis.ModeDaily, a.universeByKey("tum"))
	case "gunici100":
		go a.runManualScan(ctx, chatID, analysis.ModeIntraday, a.universeByKey("bist100"))
	case "gunicitum":
		go a.runManualScan(ctx, chatID, analysis.ModeIntraday, a.universeByKey("tum"))
	case "reset":
		_ = a.bot.SendMessage(ctx, chatID, a.cancelRunningScan())
	case "durum":
		_ = a.bot.SendMessage(ctx, chatID, a.formatStatus())
	case "ayarlar":
		_ = a.bot.SendMessage(ctx, chatID, a.formatSettings())
	default:
		if symbol, ok := a.symbolFromCommand(rawCommand); ok {
			go a.runSymbolAnalysis(ctx, chatID, symbol)
			return
		}
		_ = a.bot.SendMessage(ctx, chatID, "Bilinmeyen komut. <code>help</code> yazabilirsin.")
	}
}

func (a *App) runManualScan(ctx context.Context, chatID int64, mode analysis.Mode, universe analysis.Universe) {
	minScore := a.cfg.DailyMinScore
	title := "Gunluk Alim Radari"
	if mode == analysis.ModeIntraday {
		minScore = a.cfg.IntradayMinScore
		title = "Gun Ici Guclu Sinyal Taramasi"
	}
	_ = a.bot.SendMessage(ctx, chatID, formatScanStarted(title, universe, minScore))

	report, err := a.runScan(ctx, mode, universe, minScore)
	if err != nil {
		_ = a.bot.SendMessage(ctx, chatID, formatScanError(title, universe, err))
		return
	}
	_ = a.bot.SendMessage(ctx, chatID, formatReport(report, title))
}

func (a *App) runScheduledScan(ctx context.Context, mode analysis.Mode) {
	if a.cfg.DefaultChatID == 0 {
		log.Printf("scheduled %s scan skipped: TELEGRAM_CHAT_ID is empty", mode)
		return
	}
	universe := a.scheduledUniverse()

	minScore := a.cfg.DailyAlertMinScore
	title := "Gun Sonu Alim Radari"
	sendEmpty := true
	if mode == analysis.ModeIntraday {
		minScore = a.cfg.IntradayAlertScore
		title = "Otomatik Gun Ici Guclu Sinyal"
		sendEmpty = false
	}

	report, err := a.runScan(ctx, mode, universe, minScore)
	if err != nil {
		log.Printf("scheduled %s scan failed: %v", mode, err)
		if mode == analysis.ModeDaily {
			_ = a.bot.SendMessage(ctx, a.cfg.DefaultChatID, fmt.Sprintf("Gun sonu tarama tamamlanamadi: <code>%s</code>", html.EscapeString(err.Error())))
		}
		return
	}

	if mode == analysis.ModeIntraday {
		report.Results = a.deduper.filter(mode, universe.Key, report.Results, time.Now().In(a.cfg.MarketTimezone))
	}
	if len(report.Results) == 0 && !sendEmpty {
		log.Printf("scheduled %s scan found no new alert", mode)
		return
	}
	_ = a.bot.SendMessage(ctx, a.cfg.DefaultChatID, formatReport(report, title))
}

func (a *App) runScan(ctx context.Context, mode analysis.Mode, universe analysis.Universe, minScore int) (*analysis.Report, error) {
	select {
	case a.scanLock <- struct{}{}:
		defer func() { <-a.scanLock }()
	default:
		return nil, fmt.Errorf("baska bir tarama zaten calisiyor")
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, a.cfg.ScanTimeout)
	scanCtx, scanCancel := context.WithCancel(timeoutCtx)
	defer timeoutCancel()
	defer scanCancel()

	started := time.Now()
	log.Printf("scan started mode=%s universe=%s symbols=%d min_score=%d", mode, universe.Key, len(universe.Symbols), minScore)
	a.setRunning(mode, universe.Label, scanCancel)
	var report *analysis.Report
	var err error
	switch mode {
	case analysis.ModeDaily:
		report, err = a.scanner.ScanDaily(scanCtx, universe, minScore)
	case analysis.ModeIntraday:
		report, err = a.scanner.ScanIntraday(scanCtx, universe, minScore)
	default:
		err = fmt.Errorf("unknown scan mode %q", mode)
	}
	a.setFinished(mode, report, err)
	if err != nil {
		log.Printf("scan failed mode=%s universe=%s duration=%s error=%v", mode, universe.Key, shortDuration(time.Since(started)), err)
	} else {
		log.Printf("scan finished mode=%s universe=%s duration=%s results=%d data=%d/%d analyzed=%d", mode, universe.Key, shortDuration(time.Since(started)), len(report.Results), report.DataSymbols, report.TotalSymbols, report.AnalyzedSymbols)
	}
	return report, err
}

func (a *App) runSymbolAnalysis(ctx context.Context, chatID int64, symbol string) {
	select {
	case a.scanLock <- struct{}{}:
		defer func() { <-a.scanLock }()
	default:
		_ = a.bot.SendMessage(ctx, chatID, "Baska bir tarama veya hisse analizi calisiyor. Durdurmak icin <code>reset</code> yazabilirsin.")
		return
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 30*time.Second)
	analysisCtx, analysisCancel := context.WithCancel(timeoutCtx)
	defer timeoutCancel()
	defer analysisCancel()

	mode := analysis.Mode("hisse")
	a.setRunning(mode, symbol, analysisCancel)
	_ = a.bot.SendMessage(ctx, chatID, formatSymbolStarted(symbol))

	started := time.Now()
	log.Printf("symbol analysis started symbol=%s", symbol)
	card, err := a.scanner.AnalyzeSymbol(analysisCtx, symbol)
	a.setFinished(mode, nil, err)
	if err != nil {
		log.Printf("symbol analysis failed symbol=%s duration=%s error=%v", symbol, shortDuration(time.Since(started)), err)
		_ = a.bot.SendMessage(ctx, chatID, formatSymbolError(symbol, err))
		return
	}

	log.Printf("symbol analysis finished symbol=%s duration=%s score=%d/%d", symbol, shortDuration(time.Since(started)), card.Score, card.MaxScore)
	_ = a.bot.SendMessage(ctx, chatID, formatSymbolAnalysis(card))
}

func (a *App) scheduleLoop(ctx context.Context) {
	if a.cfg.RunScanOnStartup && a.cfg.DefaultChatID != 0 {
		go a.runScheduledScan(ctx, analysis.ModeIntraday)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkSchedule(ctx, time.Now().In(a.cfg.MarketTimezone))
		}
	}
}

func (a *App) checkSchedule(ctx context.Context, now time.Time) {
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return
	}
	if a.cfg.IntradayAutoEnabled &&
		now.Hour() >= a.cfg.IntradayStartHour &&
		now.Hour() <= a.cfg.IntradayEndHour &&
		now.Minute() == a.cfg.IntradayMinute {
		key := now.Format("2006-01-02-15")
		if key != a.lastIntradayRunKey {
			a.lastIntradayRunKey = key
			go a.runScheduledScan(ctx, analysis.ModeIntraday)
		}
	}
	if a.cfg.DailyAutoEnabled && now.Hour() == a.cfg.DailyHour && now.Minute() == a.cfg.DailyMinute {
		key := now.Format("2006-01-02")
		if key != a.lastDailyRunKey {
			a.lastDailyRunKey = key
			go a.runScheduledScan(ctx, analysis.ModeDaily)
		}
	}
}

func (a *App) setRunning(mode analysis.Mode, universe string, cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runningMode = mode
	a.runningUniverse = universe
	a.runningStartedAt = time.Now().In(a.cfg.MarketTimezone)
	a.runningCancel = cancel
}

func (a *App) setFinished(mode analysis.Mode, report *analysis.Report, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runningMode = ""
	a.runningUniverse = ""
	a.runningStartedAt = time.Time{}
	a.runningCancel = nil
	if report != nil {
		a.lastReports[mode] = report
	}
	if err != nil {
		a.lastErrors[mode] = err.Error()
	} else {
		delete(a.lastErrors, mode)
	}
}

func (a *App) cancelRunningScan() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runningMode == "" || a.runningCancel == nil {
		return "Durdurulacak aktif islem yok."
	}
	mode := a.runningMode
	universe := a.runningUniverse
	elapsed := shortDuration(time.Since(a.runningStartedAt))
	a.runningCancel()
	return fmt.Sprintf("Aktif islem durduruluyor: <code>%s / %s</code> (%s).", mode, html.EscapeString(universe), elapsed)
}

func (a *App) formatStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	var b strings.Builder
	b.WriteString("<b>Bot durumu</b>\n")
	if a.runningMode != "" {
		fmt.Fprintf(&b, "Calisan islem: <code>%s / %s</code> (%s)\n", a.runningMode, html.EscapeString(a.runningUniverse), shortDuration(time.Since(a.runningStartedAt)))
	} else {
		b.WriteString("Calisan islem: <code>yok</code>\n")
	}
	b.WriteString(a.lastLineLocked(analysis.ModeIntraday, "Son gun ici"))
	b.WriteString(a.lastLineLocked(analysis.ModeDaily, "Son gunluk"))
	return b.String()
}

func (a *App) lastLineLocked(mode analysis.Mode, label string) string {
	if report := a.lastReports[mode]; report != nil {
		return fmt.Sprintf("%s: <code>%s</code>, sonuc <code>%d</code>, sure <code>%s</code>\n",
			label,
			report.FinishedAt.Format("02.01 15:04"),
			len(report.Results),
			shortDuration(report.Duration()),
		)
	}
	if errText := a.lastErrors[mode]; errText != "" {
		return fmt.Sprintf("%s: hata <code>%s</code>\n", label, html.EscapeString(errText))
	}
	return fmt.Sprintf("%s: <code>yok</code>\n", label)
}

func (a *App) formatSettings() string {
	return fmt.Sprintf(`<b>Ayarlar</b>
Varsayilan kapsam: <code>%s</code>
Otomatik kapsam: <code>%s</code>
Kapsamlar: %s
Zaman dilimi: <code>%s</code>
Sonuc limiti: <code>%d</code>

Gun ici komut kriteri: <code>Skor >= %d</code>
Gun ici otomatik alarm: <code>Skor >= %d</code>
Gun ici plan: <code>%02d:%02d-%02d:%02d, hafta ici</code>

Gunluk komut kriteri: <code>Skor >= %d</code>
Gunluk alarm: <code>Skor >= %d</code>
Gunluk plan: <code>%02d:%02d, hafta ici</code>

Tarama motoru: <code>%s</code>
Python batch/paralellik: <code>%d/%d</code>
Tekil hisse karti: <code>TradingView snapshot</code>`,
		html.EscapeString(a.defaultUniverse().Label),
		html.EscapeString(a.scheduledUniverse().Label),
		a.formatUniverseList(),
		html.EscapeString(a.cfg.MarketTimezoneRaw),
		a.cfg.MaxResults,
		a.cfg.IntradayMinScore,
		a.cfg.IntradayAlertScore,
		a.cfg.IntradayStartHour,
		a.cfg.IntradayMinute,
		a.cfg.IntradayEndHour,
		a.cfg.IntradayMinute,
		a.cfg.DailyMinScore,
		a.cfg.DailyAlertMinScore,
		a.cfg.DailyHour,
		a.cfg.DailyMinute,
		formatPythonScannerState(a.cfg.PythonScannerEnabled),
		a.cfg.PythonScannerBatchSize,
		a.cfg.PythonScannerWorkers,
	)
}

func formatPythonScannerState(enabled bool) string {
	if enabled {
		return "Python yfinance"
	}
	return "Go TradingView"
}

type deduper struct {
	window time.Duration
	mu     sync.Mutex
	seen   map[string]time.Time
}

func newDeduper(window time.Duration) *deduper {
	return &deduper{
		window: window,
		seen:   make(map[string]time.Time),
	}
}

func (d *deduper) filter(mode analysis.Mode, universe string, signals []analysis.Signal, now time.Time) []analysis.Signal {
	if d.window <= 0 || len(signals) == 0 {
		return signals
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	filtered := make([]analysis.Signal, 0, len(signals))
	for _, signal := range signals {
		key := string(mode) + ":" + universe + ":" + signal.Symbol
		if last, ok := d.seen[key]; ok && now.Sub(last) < d.window {
			continue
		}
		d.seen[key] = now
		filtered = append(filtered, signal)
	}

	for key, last := range d.seen {
		if now.Sub(last) > d.window*2 {
			delete(d.seen, key)
		}
	}
	return filtered
}
