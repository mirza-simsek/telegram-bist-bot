import yfinance as yf
import pandas as pd
import numpy as np
from datetime import datetime
import concurrent.futures
import warnings
import os
import logging
import time

# ── 0. AYARLAR VE SUSTURUCULAR ────────────────────────────────────────
warnings.filterwarnings("ignore")
yf_logger = logging.getLogger('yfinance')
yf_logger.disabled = True
yf_logger.propagate = False
pd.options.mode.chained_assignment = None

# ── 1. YARDIMCI FONKSİYONLAR ───────────────────────────────────────────
def hisseleri_oku(dosya_adi="bist_tum_hisseler.txt"):
    if not os.path.exists(dosya_adi):
        print(f"HATA: '{dosya_adi}' bulunamadı! Lütfen hisse listesini oluşturun.")
        return []
    with open(dosya_adi, "r", encoding="utf-8") as f:
        return list(set([line.strip().upper() for line in f if line.strip()]))

# --- MATEMATİKSEL İNDİKATÖRLER ---
def ema(seri, periyot): return seri.ewm(span=periyot, adjust=False).mean()
def sma(seri, periyot): return seri.rolling(window=periyot).mean()

def rsi(seri, periyot=14):
    delta = seri.diff()
    kazan = delta.where(delta > 0, 0.0)
    kayip = -delta.where(delta < 0, 0.0)
    rs_kazan = kazan.ewm(alpha=1/periyot, adjust=False).mean()
    rs_kayip = kayip.ewm(alpha=1/periyot, adjust=False).mean()
    return 100 - (100 / (1 + rs_kazan / rs_kayip))

def intraday_vwap(df):
    """Her gün açılışta sıfırlanan kümülatif VWAP"""
    df_temp = df.copy()
    df_temp['Date'] = df.index.date
    df_temp['TP'] = (df_temp['High'] + df_temp['Low'] + df_temp['Close']) / 3
    df_temp['TP_V'] = df_temp['TP'] * df_temp['Volume']
    cum_v = df_temp.groupby('Date')['Volume'].cumsum()
    cum_tp_v = df_temp.groupby('Date')['TP_V'].cumsum()
    return cum_tp_v / cum_v

def hacim_profili_hesapla(df, lookback, bins=50, value_area_pct=0.70):
    """POC ve Value Area (VAH/VAL) Hesaplaması"""
    son = df.tail(lookback)
    fmin, fmax = son["Low"].min(), son["High"].max()
    if fmax <= fmin: return son["Close"].iloc[-1], son["Close"].iloc[-1]

    aralik = np.linspace(fmin, fmax, bins + 1)
    profil, _ = np.histogram(son["Close"], bins=bins, weights=son["Volume"])

    poc_idx = int(np.argmax(profil))
    poc = (aralik[poc_idx] + aralik[poc_idx + 1]) / 2

    toplam_hacim = np.sum(profil)
    hedef_hacim = toplam_hacim * value_area_pct
    hacim_toplami = profil[poc_idx]
    alt_idx, ust_idx = poc_idx, poc_idx

    while hacim_toplami < hedef_hacim and (alt_idx > 0 or ust_idx < bins - 1):
        alt_hacim = profil[alt_idx - 1] if alt_idx > 0 else 0
        ust_hacim = profil[ust_idx + 1] if ust_idx < bins - 1 else 0
        if alt_hacim >= ust_hacim and alt_idx > 0:
            alt_idx -= 1
            hacim_toplami += alt_hacim
        elif ust_idx < bins - 1:
            ust_idx += 1
            hacim_toplami += ust_hacim
        else:
            break

    vah = aralik[ust_idx + 1]
    return float(poc), float(vah)

def atr_hesapla(df, periyot=14):
    high, low, close_prev = df['High'], df['Low'], df['Close'].shift(1)
    tr = pd.concat([high - low, (high - close_prev).abs(), (low - close_prev).abs()], axis=1).max(axis=1)
    return tr.rolling(window=periyot).mean()

def trp_al_sinyali(close_serisi):
    if len(close_serisi) < 14: return False
    is_less = close_serisi < close_serisi.shift(4)
    return bool(is_less.iloc[-9:].all())

def obv_hacim_sinyali(df):
    delta = df["Close"].diff()
    yon = np.where(delta > 0, 1, np.where(delta < 0, -1, 0))
    obv_cizgisi = (yon * df["Volume"]).cumsum()
    obv_sma = obv_cizgisi.rolling(21).mean()
    return bool(obv_cizgisi.iloc[-1] > obv_sma.iloc[-1] and obv_cizgisi.iloc[-2] <= obv_sma.iloc[-2])

# ── 2. ÇEKİRDEK HESAPLAMA MOTORU ──────────────────────────────────────
def metrikleri_hesapla(df, poc_lookback):
    close, high, volume = df["Close"], df["High"], df["Volume"]

    res = {}
    res['Fiyat'] = float(close.iloc[-1])
    res['S20'] = float(sma(close, 20).iloc[-1])
    res['S50'] = float(sma(close, 50).iloc[-1])
    res['S200'] = float(sma(close, 200).iloc[-1])
    res['RSI'] = float(rsi(close).iloc[-1])
    res['VWAP'] = float(intraday_vwap(df).iloc[-1])
    res['POC'], res['VAH'] = hacim_profili_hesapla(df, lookback=poc_lookback)
    res['ATR'] = float(atr_hesapla(df).iloc[-1])

    res['TRP'] = trp_al_sinyali(close)
    res['OBV'] = obv_hacim_sinyali(df)

    ort_hacim = volume.iloc[-21:-1].mean()
    res['HacimX'] = float(volume.iloc[-1] / ort_hacim) if ort_hacim > 0 else 0

    return res

# ── 3. OPTİMİZASYON: TOPLU VERİ İNDİRME ─────────────────────────────
def toplu_veri_indir(hisseler, period, interval, max_retries=2):
    """
    Birden fazla hisseyi tek seferde indir.
    Döndürür: {ticker: DataFrame} sözlüğü
    """
    semboller = [f"{t}.IS" for t in hisseler]

    for deneme in range(max_retries):
        try:
            raw = yf.download(
                semboller,
                period=period,
                interval=interval,
                progress=False,
                ignore_tz=True,
                group_by="ticker",
                threads=True,
            )
            break
        except Exception:
            if deneme == max_retries - 1:
                return {}
            time.sleep(1)

    if raw is None or raw.empty:
        return {}

    sonuc = {}
    for ticker, sembol in zip(hisseler, semboller):
        try:
            if isinstance(raw.columns, pd.MultiIndex):
                df = raw[sembol].copy() if sembol in raw.columns.get_level_values(0) else raw.xs(sembol, axis=1, level=1).copy()
            else:
                df = raw.copy()

            df.columns = [c.strip().title() for c in df.columns]
            df.dropna(subset=["Close", "Volume"], inplace=True)

            if len(df) > 0:
                sonuc[ticker] = df
        except Exception:
            pass

    return sonuc

# ── 4. ANALİZ MOTORU (Artık Sadece Hesaplama Yapar, Network Yok) ──────
def analiz_et(ticker, data_1h, data_15m):
    """
    Veri önceden indirilmiş olarak gelir — bu fonksiyon sadece hesap yapar.
    """
    try:
        if data_1h is None or data_15m is None:
            return None
        if len(data_1h) < 200 or len(data_15m) < 200:
            return None

        m_1h  = metrikleri_hesapla(data_1h,  poc_lookback=80)
        m_15m = metrikleri_hesapla(data_15m, poc_lookback=40)

        F = m_1h['Fiyat']
        skor = 0
        detaylar = []

        onay_1h  = F > m_1h['VWAP']  and F > m_1h['POC']
        onay_15m = F > m_15m['VWAP'] and F > m_15m['POC']

        if onay_1h:  skor += 2
        if onay_15m: skor += 2

        if F > m_1h['S20'] and m_1h['S20'] > m_1h['S50']:
            skor += 1; detaylar.append("1h Boğa Trendi 📈")

        if F < m_1h['S200']:
            skor -= 2; detaylar.append("1h SMA200 Altı ⚠️")

        if m_1h['TRP'] or m_15m['TRP']:
            skor += 1; detaylar.append("TRP(9) AL 🟢")

        if m_1h['OBV']:  skor += 1; detaylar.append("1h OBV🚀")
        if m_15m['OBV']: skor += 1; detaylar.append("15m OBV🚀")

        if F > m_1h['VAH']: skor += 1; detaylar.append("1h VAH Kırılımı 🔥")
        if m_1h['HacimX'] > 1.5 or m_15m['HacimX'] > 1.5:
            skor += 1; detaylar.append("Hacim Sıçraması 🌊")

        stop_loss = F - (1.5 * m_1h['ATR'])

        return {
            "Hisse":      ticker,
            "Fiyat":      round(F, 2),
            "Skor":       skor,
            "StopLoss":   round(stop_loss, 2),
            "VWAP_1h":    "Üst ✓" if F > m_1h['VWAP']  else "Alt ✗",
            "POC_1h":     "Üst ✓" if F > m_1h['POC']   else "Alt ✗",
            "VWAP_15m":   "Üst ✓" if F > m_15m['VWAP'] else "Alt ✗",
            "POC_15m":    "Üst ✓" if F > m_15m['POC']  else "Alt ✗",
            "RSI_1h":     round(m_1h['RSI'], 1),
            "RSI_15m":    round(m_15m['RSI'], 1),
            "HacimX_1h":  round(m_1h['HacimX'], 1),
            "HacimX_15m": round(m_15m['HacimX'], 1),
            "Sinyaller":  " | ".join(detaylar)
        }
    except Exception:
        return None

# ── 5. ANA ÇALIŞTIRMA BLOĞU ────────────────────────────────────────────
if __name__ == "__main__":
    hisseler = hisseleri_oku()
    if not hisseler:
        exit()

    print(f"\n{'='*140}")
    print(f"  BIST ÇİFT ZAMAN DİLİMİ (1h & 15m) PROFESYONEL TARAMA — {datetime.now().strftime('%d.%m.%Y %H:%M')}")
    print(f"  Hesaplanan İndikatörler: VWAP, POC/VAH, SMA Boğa Dizilimi, TRP, OBV, RSI, HacimX, ATR Stop-Loss")
    print(f"{'='*140}\n")

    BATCH_BOYUTU = 50
    PARALEL_GRUP = 4

    gruplar = [hisseler[i:i+BATCH_BOYUTU] for i in range(0, len(hisseler), BATCH_BOYUTU)]

    tum_1h:  dict[str, pd.DataFrame] = {}
    tum_15m: dict[str, pd.DataFrame] = {}

    print(f"  Toplam {len(hisseler)} hisse → {len(gruplar)} grup × 2 periyot = {len(gruplar)*2} görev")
    print(f"  Paralel indirme başlatılıyor (aynı anda {PARALEL_GRUP*2} bağlantı)...\n")

    indirme_lock = __import__('threading').Lock()
    tamamlanan_grup = [0]

    def grup_indir(args):
        g_idx, grup = args
        r1h  = toplu_veri_indir(grup, period="3mo", interval="1h")
        r15m = toplu_veri_indir(grup, period="1mo", interval="15m")
        with indirme_lock:
            tum_1h.update(r1h)
            tum_15m.update(r15m)
            tamamlanan_grup[0] += 1
            print(
                f"  ✓ Grup {g_idx:>2}/{len(gruplar)} tamamlandı "
                f"(1h:{len(r1h)} / 15m:{len(r15m)} hisse)  "
                f"[{tamamlanan_grup[0]*100//len(gruplar):>3}%]"
            )
        return g_idx

    t_indir_baslangic = time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=PARALEL_GRUP) as dl_exec:
        list(dl_exec.map(grup_indir, enumerate(gruplar, 1)))

    print(f"\n  İndirme tamamlandı: {time.time()-t_indir_baslangic:.1f}s  "
          f"({len(tum_1h)} hisse 1h | {len(tum_15m)} hisse 15m)\n")

    print(f"\n  Analiz hesaplanıyor ({len(tum_1h)} hisse)...\n")

    hesaplanacaklar = [
        (t, tum_1h.get(t), tum_15m.get(t))
        for t in hisseler
        if t in tum_1h and t in tum_15m
    ]

    sonuclar = []
    max_workers = min(32, (os.cpu_count() or 4) * 4)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(analiz_et, t, d1h, d15m): t
            for t, d1h, d15m in hesaplanacaklar
        }
        tamamlanan = 0
        for future in concurrent.futures.as_completed(futures):
            tamamlanan += 1
            print(f"  [{tamamlanan:03d}/{len(hesaplanacaklar)}] Analiz ediliyor: {futures[future]:<8}", end="\r", flush=True)
            res = future.result()
            if res:
                sonuclar.append(res)

    df = pd.DataFrame(sonuclar)
    print(" " * 80, end="\r")

    if not df.empty:
        df_final = df[
            (df["Skor"] >= 5) &
            (df["RSI_1h"] < 80) &
            (df["RSI_15m"] < 80)
            ].copy()
        df_final = df_final.sort_values(
            by=["Skor", "RSI_1h"], ascending=[False, True]
        ).head(10)

        print(f"\n{'='*140}")
        print(f"  KRİTERLERE UYAN GÜNÜN EN GÜÇLÜ HİSSELERİ")
        print(f"{'='*140}\n")

        if df_final.empty:
            print("  Şu an bu zorlu çift-onay (Dual Confirmation) şartlarını karşılayan hisse bulunamadı.\n")
        else:
            baslik = (
                f"  {'HİSSE':<6} | {'SKOR':<5} | {'FİYAT':<8} | {'STOP-LS':<7} | "
                f"{'1h VWAP':<7} | {'1h POC':<7} | {'15m VWAP':<8} | {'15m POC':<7} | "
                f"{'1h RSI':<6} | {'15m RSI':<7} | {'1h Hacim':<8} | {'15m Hacim':<9} | {'SİNYALLER'}"
            )
            print(baslik)
            print("  " + "-"*138)
            for _, r in df_final.iterrows():
                print(
                    f"  {r['Hisse']:<6} | {r['Skor']:>2}/10 | {r['Fiyat']:>8.2f} | "
                    f"{r['StopLoss']:>7.2f} | {r['VWAP_1h']:<7} | {r['POC_1h']:<7} | "
                    f"{r['VWAP_15m']:<8} | {r['POC_15m']:<7} | {r['RSI_1h']:>6.1f} | "
                    f"{r['RSI_15m']:>7.1f} | x{r['HacimX_1h']:<7.1f} | x{r['HacimX_15m']:<8.1f} | "
                    f"{r['Sinyaller']}"
                )
        print("\n" + "="*140 + "\n")
    else:
        print("\n\n  Hata: Hiçbir hissenin verisi indirilemedi.\n")
