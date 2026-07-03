import concurrent.futures
import logging
import os
import time
import warnings
from datetime import datetime

import numpy as np
import pandas as pd
import yfinance as yf


# -- 0. AYARLAR VE SUSTURUCULAR ----------------------------------------
warnings.filterwarnings("ignore")
yf_logger = logging.getLogger("yfinance")
yf_logger.disabled = True
yf_logger.propagate = False
pd.options.mode.chained_assignment = None


# -- 1. YARDIMCI FONKSIYONLAR ------------------------------------------
def hisseleri_oku(dosya_adi="bist_tum_hisseler.txt"):
    if not os.path.exists(dosya_adi):
        print(f"HATA: '{dosya_adi}' bulunamadi! Lutfen hisse listesini olusturun.")
        return []
    with open(dosya_adi, "r", encoding="utf-8") as f:
        return list(set([line.strip().upper() for line in f if line.strip()]))


def ema(seri, periyot):
    return seri.ewm(span=periyot, adjust=False).mean()


def sma(seri, periyot):
    return seri.rolling(window=periyot).mean()


def rsi(seri, periyot=14):
    delta = seri.diff()
    kazan = delta.where(delta > 0, 0.0)
    kayip = -delta.where(delta < 0, 0.0)
    rs_kazan = kazan.ewm(alpha=1 / periyot, adjust=False).mean()
    rs_kayip = kayip.ewm(alpha=1 / periyot, adjust=False).mean()
    return 100 - (100 / (1 + rs_kazan / rs_kayip))


def vwap_20gun(df):
    tp = (df["High"] + df["Low"] + df["Close"]) / 3
    return (tp * df["Volume"]).rolling(20).sum() / df["Volume"].rolling(20).sum()


def poc_hesapla(df, lookback=22, bins=50):
    son = df.tail(lookback)
    fmin, fmax = son["Low"].min(), son["High"].max()
    if fmax <= fmin:
        return son["Close"].iloc[-1]
    aralik = np.linspace(fmin, fmax, bins + 1)
    profil, _ = np.histogram(son["Close"], bins=bins, weights=son["Volume"])
    idx = int(np.argmax(profil))
    return (aralik[idx] + aralik[idx + 1]) / 2


def atr_hesapla(df, periyot=14):
    high, low, close_prev = df["High"], df["Low"], df["Close"].shift(1)
    tr = pd.concat([high - low, (high - close_prev).abs(), (low - close_prev).abs()], axis=1).max(axis=1)
    return tr.rolling(window=periyot).mean()


def trp_al_sinyali(close_serisi):
    if len(close_serisi) < 14:
        return False
    is_less = close_serisi < close_serisi.shift(4)
    return bool(is_less.iloc[-9:].all())


def obv_hacim_sinyali(df):
    delta = df["Close"].diff()
    yon = np.where(delta > 0, 1, np.where(delta < 0, -1, 0))
    obv_cizgisi = (yon * df["Volume"]).cumsum()
    obv_sma = obv_cizgisi.rolling(21).mean()
    return bool(obv_cizgisi.iloc[-1] > obv_sma.iloc[-1] and obv_cizgisi.iloc[-2] <= obv_sma.iloc[-2])


def pivot_s3_durumu(df):
    dun = df.iloc[-2]
    h, l, c = float(dun["High"]), float(dun["Low"]), float(dun["Close"])
    pivot = (h + l + c) / 3
    s3 = pivot - (2 * (h - l))
    fiyat = float(df["Close"].iloc[-1])
    return s3, (fiyat >= s3 * 0.98) and (fiyat <= s3 * 1.02), fiyat > s3


# -- 2. TOPLU VERI INDIRME ---------------------------------------------
def toplu_veri_indir(hisseler, period="1y", interval="1d", max_retries=2):
    semboller = [f"{t}.IS" for t in hisseler]

    for deneme in range(max_retries):
        try:
            raw = yf.download(
                semboller,
                period=period,
                interval=interval,
                auto_adjust=True,
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
                if sembol in raw.columns.get_level_values(0):
                    df = raw[sembol].copy()
                else:
                    df = raw.xs(sembol, axis=1, level=1).copy()
            else:
                df = raw.copy()

            df.columns = [c.strip().title() for c in df.columns]
            for col in ["Open", "High", "Low", "Close", "Volume"]:
                if col not in df.columns:
                    raise KeyError(col)
                df[col] = pd.to_numeric(df[col], errors="coerce").astype(float)
            df.dropna(subset=["Close", "Volume"], inplace=True)

            if len(df) >= 200:
                sonuc[ticker] = df
        except Exception:
            pass

    return sonuc


def veri_indir(ticker):
    data = toplu_veri_indir([ticker], period="1y", interval="1d")
    return ticker, data.get(ticker)


# -- 3. ANALIZ MOTORU ---------------------------------------------------
def hisse_analiz(ticker, df):
    try:
        close, volume = df["Close"], df["Volume"]

        s20 = sma(close, 20)
        s50 = sma(close, 50)
        s200 = sma(close, 200)
        rsi_s = rsi(close)
        vwap_s = vwap_20gun(df)
        poc = poc_hesapla(df, lookback=22)
        atr = atr_hesapla(df)

        f = float(close.iloc[-1])
        f_s20 = float(s20.iloc[-1])
        f_s50 = float(s50.iloc[-1])
        f_s200 = float(s200.iloc[-1])
        f_rsi = float(rsi_s.iloc[-1])
        f_vwap = float(vwap_s.iloc[-1])
        f_atr = float(atr.iloc[-1])

        son_hacim = float(volume.iloc[-1])
        simdi = datetime.now()
        if 10 <= simdi.hour < 18:
            gecen_dakika = ((simdi.hour - 10) * 60) + simdi.minute
            hesaplanan_hacim = son_hacim * (480 / max(gecen_dakika, 1))
        else:
            hesaplanan_hacim = son_hacim

        ort_hacim = volume.iloc[-21:-1].mean()
        hacim_orani = float(hesaplanan_hacim / ort_hacim) if ort_hacim > 0 else 0

        trp_sinyal = trp_al_sinyali(close)
        obv_sinyal = obv_hacim_sinyali(df)
        _, s3_yakin_mi, s3_ustunde_mi = pivot_s3_durumu(df)

        skor = 0
        detaylar = []

        if f < f_s200:
            skor -= 2
            detaylar.append("Ayi Trendi (SMA200 Alti)")
        else:
            skor += 1
            detaylar.append("Boga")

        if trp_sinyal:
            skor += 3
            detaylar.append("TRP(9) AL")
        if obv_sinyal:
            skor += 2
            detaylar.append("OBV Kirilim")
        if s3_yakin_mi and s3_ustunde_mi:
            skor += 1
            detaylar.append("S3 Destek")
        if f > f_s20 and f_s20 > f_s50:
            skor += 1
            detaylar.append("Kusursuz Trend (F>S20>S50)")
        if 50 < f_rsi < 70:
            skor += 1
            detaylar.append("Ideal RSI")
        if hacim_orani > 1.5:
            skor += 1
            detaylar.append(f"HacimX{hacim_orani:.1f}")

        onay = "GUVENLI" if (f > poc and f > f_vwap) else "RISKLI"
        stop_loss = f - (1.5 * f_atr)

        return {
            "Hisse": ticker,
            "Fiyat": round(f, 2),
            "Skor": skor,
            "RSI": round(f_rsi, 1),
            "HacimX": round(hacim_orani, 2),
            "StopLoss": round(stop_loss, 2),
            "Onay": onay,
            "Sinyal": " | ".join(detaylar),
        }
    except Exception:
        return None


# -- 4. ANA CALISTIRMA BLOGU -------------------------------------------
if __name__ == "__main__":
    hisseler = hisseleri_oku()
    if not hisseler:
        exit()

    print(f"\n{'=' * 130}")
    print(f"  BIST GUCLU 'AL' SINYALI TARAMASI - {datetime.now().strftime('%d.%m.%Y %H:%M')}")
    print(f"  Toplam {len(hisseler)} hisse taraniyor...")
    print(f"{'=' * 130}\n")

    batch_boyutu = 50
    paralel_grup = 4
    gruplar = [hisseler[i:i + batch_boyutu] for i in range(0, len(hisseler), batch_boyutu)]
    tum_data = {}

    def grup_indir(args):
        g_idx, grup = args
        data = toplu_veri_indir(grup, period="1y", interval="1d")
        tum_data.update(data)
        print(f"  Grup {g_idx:>2}/{len(gruplar)} tamamlandi ({len(data)} hisse)")

    t_baslangic = time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=paralel_grup) as dl_exec:
        list(dl_exec.map(grup_indir, enumerate(gruplar, 1)))

    print(f"\n  Indirme tamamlandi: {time.time() - t_baslangic:.1f}s ({len(tum_data)} hisse)\n")
    print(f"  Analiz hesaplanıyor ({len(tum_data)} hisse)...\n")

    final_list = []
    max_workers = min(32, (os.cpu_count() or 4) * 4)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {executor.submit(hisse_analiz, t, df): t for t, df in tum_data.items()}
        tamamlanan = 0
        for future in concurrent.futures.as_completed(futures):
            tamamlanan += 1
            print(f"  [{tamamlanan:03d}/{len(tum_data)}] Analiz ediliyor: {futures[future]:<8}", end="\r", flush=True)
            res = future.result()
            if res:
                final_list.append(res)

    df_sonuc = pd.DataFrame(final_list)
    print(" " * 80, end="\r")

    if not df_sonuc.empty:
        df_filtreli = df_sonuc[(df_sonuc["Skor"] >= 3) & (df_sonuc["RSI"] < 80)].copy()
        df_ilk_10 = df_filtreli.sort_values(
            by=["Skor", "HacimX", "RSI"], ascending=[False, False, True]
        ).head(10)

        print(f"\n{'=' * 130}")
        print("  GUNUN POTANSIYELI EN YUKSEK 10 HISSESI")
        print(f"{'=' * 130}\n")

        if df_ilk_10.empty:
            print("  Sistemdeki sartlari karsilayan hisse bulunamadi.\n")
        else:
            baslik = f"  {'HISSE':<6} | {'SKOR':<5} | {'FIYAT':<8} | {'STOP-LOSS':<9} | {'RSI':<5} | {'HACIM':<6} | {'ONAY':<8} | SINYALLER"
            print(baslik)
            print("  " + "-" * 128)
            for _, row in df_ilk_10.iterrows():
                print(
                    f"  {row['Hisse']:<6} | {row['Skor']:>2}/10 | {row['Fiyat']:>8.2f} | "
                    f"{row['StopLoss']:>9.2f} | {row['RSI']:>5.1f} | x{row['HacimX']:<5.1f} | "
                    f"{row['Onay']:<8} | {row['Sinyal']}"
                )
        print("\n")
    else:
        print("\n\n  Hata: Hicbir hissenin verisi indirilemedi.\n")
