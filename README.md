# BIST30 Telegram Teknik Tarama Botu

Bu bot yalnizca BIST30 hisselerini tarar. Manuel ve otomatik taramalar ile tekil hisse karti ayni Python analiz motorunu kullanir.

## Komutlar ve zamanlama

- `/gunici`: Tamamlanmis 5 dakikalik ve 15 dakikalik **TL** mumlariyla gun ici tarama.
- `/gunluk`: Gunluk **TL ve USD** grafiklerini bagimsiz puanlayan tarama.
- `/THYAO` gibi BIST30 sembolleri: Gun ici TL, gunluk TL ve gunluk USD bolumlu hisse karti.
- `/durum`, `/ayarlar`, `/reset`, `/help`: Durum, ayarlar, aktif islemi durdurma ve yardim.

Otomatik gun ici tarama hafta ici kapanan her 5 dakikalik mumdan 1 dakika sonra calisir. Otomatik gunluk tarama Istanbul saatine gore hafta ici **22:30**'dadir. Bu saat sabittir; eski `.env` dosyalarindaki 18:20 ayari artik kullanilmaz. Yeni gun ici sinyaller tekrar bildirimini azaltmak icin deduplikasyondan gecer.

## Analiz kurallari

Ortak motor `scripts/analysis_engine.py` dosyasindadir. EMA/SMA, RSI, MACD, CMF, goreli hacim, VWAP, cift range filter, onayli pivot destek/direnc ve RSI/MACD uyumsuzlugunu hesaplar. Destek bolgesindeki tepki mumu ve trend donusu durum etiketini etkiler. Pivotun sagindaki mumlar tamamlanmadan pivot sinyale girmez; acik mum kullanilmaz. Kapali kaynak 5'i Bir Arada gostergesinin formulu acik olmadigi icin range filter ayarlari gozlemlere dayali aday bir yeniden kurulumdur; orijinalle birebir eslesme iddiasi yoktur.

TL ve USD puanlari 0-10 araliginda **ayri** gosterilir; agirlikli bir puan uretilmez. Gun ici analiz USD verisi indirmez. Gunluk USD kapanisi `hisse TL kapanisi / USDTRY kapanisi` ile hesaplanir. USD veri kalitesi ilk surumde `close_only` olarak isaretlenir; hacim ve OHLC tabanli mum teyidi USD puanina eklenmez.

Bu sinyallerin %80-90 isabetle calisacagi iddia edilmez. Bu hedef, gecmis veri uzerinde tarihsel BIST30 uyeligi, islem maliyetleri ve gercekci giris/cikis kurallariyla yapilacak ileriye donuk testlerden sonra olculebilir.

## Kurulum

Python 3.10+ ve Go 1.22+ gerekir. `.env.example` dosyasini `.env` olarak kopyalayip Telegram bot tokenini ve chat ID'sini ayarlayin. Python bagimliliklarini yukleyin:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
```

`.env` icinde `PYTHON_EXECUTABLE=.venv/bin/python` olarak ayarlayin. Botu calistirin:

```bash
go run ./cmd/bistbot
```

Varsayilan kapsam `data/bist_30_hisseler.txt` dosyasidir. Liste `BIST30_SYMBOLS_FILE` ile guncellenebilir. `PYTHON_SCANNER_ENABLED=true` tutulmalidir; ortak TL/USD motoru bu modda kullanilir. Manuel ve otomatik gunluk tarama ayni hesaplama kurallarini kullanir, sadece listeleme esikleri ayridir.

## Test

```bash
.venv/bin/python -m unittest discover -s scripts -p 'test_analysis_engine.py'
go test ./...
```

Canli yayin icin mevcut Docker Compose akisi `infra/DEPLOY.md` dosyasindadir. Bu depodaki degisiklikler canli sunucuya kendiliginden uygulanmaz.
