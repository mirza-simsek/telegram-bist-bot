# Telegram BIST Bot

Go ile yazilmis Telegram BIST teknik tarama botu.

Tarama mantigi `scripts/gun_ici_tarama.py` ve `scripts/gunluk_tarama.py` dosyalarinda, Telegram botuna baglandi:

- `gunici100` ve `gunicitum`: Python/yfinance motoruyla 1h + 15m cift zaman dilimi taramasi. VWAP, POC/VAH, SMA20/50/200, RSI, TRP(9), OBV ve hacim sicramasi kullanir.
- `gunluk100` ve `gunluktum`: Python/yfinance motoruyla 1y/1d gunluk alim radari. SMA20/50/200, RSI, 20 gun VWAP, POC, pivot S3, TRP(9), OBV ve hacim projeksiyonu kullanir.
- `ALARK` gibi tekil hisse komutlari: 15dk, 1s ve gunluk teknik kart uretir. EMA9/20, VWAP, RSI, SMA200, hacim ve teknik skor birlikte okunur.

Bot ayrica hafta ici otomatik calisir:

- Gun ici: varsayilan olarak 10:15-17:15 arasinda saatte bir tarar. Sadece cok guclu sinyal varsa bildirim gonderir.
- Gun sonu: varsayilan olarak 18:20'de gunluk radar bildirimi gonderir.

Bu bot teknik tarama aracidir; yatirim tavsiyesi degildir.

## Kurulum

1. BotFather'dan Telegram bot token'i al.
2. `.env.example` dosyasini `.env` olarak kopyala.
3. `.env` icinde `TELEGRAM_BOT_TOKEN` alanini doldur.
4. Chat ID'ni bilmiyorsan botu bir kez calistirip Telegram'da `/start` yaz. Terminal log'unda veya bot cevabinda chat ID'yi gorup `.env` icindeki `TELEGRAM_CHAT_ID` alanina yaz.
5. Botu calistir:

```bash
go run ./cmd/bistbot
```

veya:

```bash
./scripts/run.sh
```

## Komutlar

- `gunici100`: Sadece BIST 100 hisselerinde gun ici tarama yapar.
- `gunicitum`: BIST Tum listesinde gun ici tarama yapar.
- `gunluk100`: Sadece BIST 100 hisselerinde gunluk radar calistirir.
- `gunluktum`: BIST Tum listesinde gunluk radar calistirir.
- `ALARK`: ALARK icin 15dk, 1s ve gunluk teknik kart hazirlar. BIST listesindeki diger semboller de ayni sekilde kullanilir.
- `reset`: O anda calisan tarama veya hisse analizini durdurur.
- `durum`: Calisan/son tarama durumunu gosterir.
- `ayarlar`: Esikleri ve zamanlama ayarlarini gosterir.
- `help`: Kisa yardim mesajini gosterir.

Genel tarama raporlari on eleme listesi olarak sade tutulur: hisse, fiyat ve skor gosterir. Ayrintili teknik yorum icin tekil hisse komutu kullanilir.

Raporlarda otomatik stop seviyesi gosterilmez. Ilk prototipte `fiyat - 1.5 * ATR` referansi vardi; bu genel volatilite referansi oldugu icin gercek risk yonetimi stop'u gibi sunulmasi dogru degildi. Stop karari pozisyon boyutu, vade, portfoy riski ve stratejiye gore ayrica belirlenmelidir.

## Onemli Ayarlar

`.env` icinde en cok degistirecegin alanlar:

```env
ALL_SYMBOLS_FILE=data/bist_tum_hisseler.txt
BIST100_SYMBOLS_FILE=data/bist_100_hisseler.txt
DEFAULT_UNIVERSE=tum
SCHEDULED_UNIVERSE=tum
MAX_RESULTS=10

PYTHON_SCANNER_ENABLED=true
PYTHON_EXECUTABLE=python3
PYTHON_SCANNER_SCRIPT=scripts/bist_data_scrap_bridge.py
PYTHON_SCANNER_BATCH_SIZE=50
PYTHON_SCANNER_WORKERS=4
PYTHON_SCANNER_YF_THREADS=false

GUNLUK_MIN_SCORE=3
GUNICI_MIN_SCORE=5
GUNLUK_ALERT_MIN_SCORE=3
GUNICI_ALERT_MIN_SCORE=7

INTRADAY_SCAN_MINUTE=15
DAILY_SCAN_HOUR=18
DAILY_SCAN_MINUTE=20
```

Daha hizli ve daha az istek atan bir varsayilan kurulum istersen:

```env
DEFAULT_UNIVERSE=bist100
SCHEDULED_UNIVERSE=bist100
```

## Veri Kaynagi ve Mimari

Bot iki parca gibi calisir:

1. Go uygulamasi Telegram komutlarini, otomatik zamanlamayi, `reset` islemini, yetki kontrolunu ve mesaj formatlarini yonetir.
2. `scripts/bist_data_scrap_bridge.py`, ayni dizindeki `gun_ici_tarama.py` ve `gunluk_tarama.py` dosyalarini import eder, tarama mantigini calistirir ve sonucu JSON olarak Go uygulamasina verir.

Bu iki Python dosyasi repoya dahildir (vendored); ayri bir proje/dizin gerektirmez. Telegram bot bu dosyalari degistirmez; sadece ciktisini Telegram'in bekledigi rapor formatina cevirir.

Hisseler 50'lik gruplarla toplu indirilir. Varsayilan olarak 4 download worker kullanilir; bu, eski `bist_data_scrap` taramalarindaki buyuk liste akisina denk gelir.

Tekil hisse kartlari (`ALARK` gibi) halen TradingView scanner snapshot kullanir. Bu kartta veri kalite kontrolu uygulanir:

- Fiyat sifir/negatif veya asiri buyukse analiz reddedilir.
- RSI 0-100 araliginda degilse kullanilmaz.
- `Recommend.All` -1 ile +1 araliginda degilse kullanilmaz.
- Hacim orani negatif veya asiri buyukse kullanilmaz.
- EMA/VWAP/SMA seviyeleri fiyatla mantiksiz oranda uyumsuzsa skorlamaya alinmaz.

## Production (Docker Compose)

Canli yayin artik Docker Compose ile, git tabanli bir deploy akisiyla yapiliyor. Detayli kurulum ve gunluk deploy komutlari icin bkz. [`infra/DEPLOY.md`](infra/DEPLOY.md).

Ozet:

```bash
git pull --ff-only
docker compose --env-file infra/.env.production -f infra/compose.prod.yml up -d --build bistbot
```

### Legacy (deprecated): Oracle/Systemd Yayini

Asagidaki systemd tabanli yayin yontemi artik kullanilmiyor, sadece gecis donemi icin rollback amacli belgelenmistir. Yeni kurulumlar icin `infra/DEPLOY.md` kullanilmalidir.

Tek komutluk yayin icin:

```bash
SSH_HOST=<oracle-public-ip> SSH_USER=ubuntu SSH_KEY=~/.ssh/<key> REMOTE_ARCH=arm64 ./scripts/deploy_oracle.sh
```

`REMOTE_ARCH` Oracle sunucunun mimarisine gore `arm64` veya `amd64` olabilir. Script Linux binary uretir, Telegram botu `/opt/telegram-bist-bot` altina, `bist_data_scrap` projesini `/opt/bist_data_scrap` altina kopyalar, Python venv kurar ve `bistbot` systemd servisini baslatir.

Loglari izlemek icin:

```bash
sudo journalctl -u bistbot -f
```

Sunucuya elle girip kurmak istersen repo kopyalandiktan sonra alternatif komut:

```bash
sudo APP_DIR=/opt/telegram-bist-bot BIST_DATA_SCRAP_SRC=/path/to/bist_data_scrap BIST_DATA_SCRAP_DIR=/opt/bist_data_scrap ./scripts/install_systemd_service.sh
sudo systemctl restart bistbot
```

## Derleme

```bash
go build ./cmd/bistbot
```

Binary olustuktan sonra:

```bash
./bistbot
```

## macOS'ta Surekli Calistirma

Yerel makinede yfinance taramalari icin LaunchAgent yerine `screen` oturumu kullan:

```bash
./scripts/start_screen.sh
./scripts/status_screen.sh
./scripts/stop_screen.sh
```

`start_screen.sh` binary'yi yeniden derler ve botu `bistbot` adli detached screen oturumunda baslatir. Loglar `logs/bistbot.shell.err.log` ve `logs/bistbot.supervisor.log` dosyalarina yazilir.

LaunchAgent bu projede onerilmez; macOS LaunchAgent altinda yfinance ayni komutta cok dusuk veri kapsami dondurebildi.
