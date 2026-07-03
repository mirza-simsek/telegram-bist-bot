#!/usr/bin/env python3
import argparse
import concurrent.futures
import json
import logging
import math
import os
import sys
import time
import warnings
from datetime import datetime
from types import SimpleNamespace
from zoneinfo import ZoneInfo

import numpy as np
import pandas as pd
import yfinance as yf


warnings.filterwarnings("ignore")
yf_logger = logging.getLogger("yfinance")
yf_logger.disabled = True
yf_logger.propagate = False
pd.options.mode.chained_assignment = None


def read_symbols(path):
    symbols = []
    seen = set()
    with open(path, "r", encoding="utf-8") as handle:
        for line in handle:
            symbol = line.strip().upper()
            if not symbol or symbol.startswith("#") or symbol in seen:
                continue
            seen.add(symbol)
            symbols.append(symbol)
    return symbols


def sma(series, period):
    return series.rolling(window=period).mean()


def rsi(series, period=14):
    delta = series.diff()
    gain = delta.where(delta > 0, 0.0)
    loss = -delta.where(delta < 0, 0.0)
    avg_gain = gain.ewm(alpha=1 / period, adjust=False).mean()
    avg_loss = loss.ewm(alpha=1 / period, adjust=False).mean()
    return 100 - (100 / (1 + avg_gain / avg_loss))


def intraday_vwap(df):
    temp = df.copy()
    temp["Date"] = temp.index.date
    temp["TP"] = (temp["High"] + temp["Low"] + temp["Close"]) / 3
    temp["TPV"] = temp["TP"] * temp["Volume"]
    cum_volume = temp.groupby("Date")["Volume"].cumsum()
    cum_tpv = temp.groupby("Date")["TPV"].cumsum()
    return cum_tpv / cum_volume


def rolling_vwap(df, period=20):
    tp = (df["High"] + df["Low"] + df["Close"]) / 3
    return (tp * df["Volume"]).rolling(period).sum() / df["Volume"].rolling(period).sum()


def volume_profile(df, lookback, bins=50, value_area_pct=0.70):
    last = df.tail(lookback)
    low = last["Low"].min()
    high = last["High"].max()
    if high <= low:
        price = last["Close"].iloc[-1]
        return float(price), float(price)

    edges = np.linspace(low, high, bins + 1)
    profile, _ = np.histogram(last["Close"], bins=bins, weights=last["Volume"])
    poc_idx = int(np.argmax(profile))
    poc = (edges[poc_idx] + edges[poc_idx + 1]) / 2

    total_volume = np.sum(profile)
    target_volume = total_volume * value_area_pct
    volume_sum = profile[poc_idx]
    lower_idx = poc_idx
    upper_idx = poc_idx

    while volume_sum < target_volume and (lower_idx > 0 or upper_idx < bins - 1):
        lower_volume = profile[lower_idx - 1] if lower_idx > 0 else 0
        upper_volume = profile[upper_idx + 1] if upper_idx < bins - 1 else 0
        if lower_volume >= upper_volume and lower_idx > 0:
            lower_idx -= 1
            volume_sum += lower_volume
        elif upper_idx < bins - 1:
            upper_idx += 1
            volume_sum += upper_volume
        else:
            break

    vah = edges[upper_idx + 1]
    return float(poc), float(vah)


def atr(df, period=14):
    high = df["High"]
    low = df["Low"]
    prev_close = df["Close"].shift(1)
    true_range = pd.concat(
        [high - low, (high - prev_close).abs(), (low - prev_close).abs()],
        axis=1,
    ).max(axis=1)
    return true_range.rolling(window=period).mean()


def trp_buy_signal(close):
    if len(close) < 14:
        return False
    is_less = close < close.shift(4)
    return bool(is_less.iloc[-9:].all())


def obv_signal(df):
    delta = df["Close"].diff()
    direction = np.where(delta > 0, 1, np.where(delta < 0, -1, 0))
    obv = (direction * df["Volume"]).cumsum()
    obv_sma = obv.rolling(21).mean()
    return bool(obv.iloc[-1] > obv_sma.iloc[-1] and obv.iloc[-2] <= obv_sma.iloc[-2])


def pivot_s3_status(df):
    previous = df.iloc[-2]
    high = float(previous["High"])
    low = float(previous["Low"])
    close = float(previous["Close"])
    pivot = (high + low + close) / 3
    s3 = pivot - (2 * (high - low))
    price = float(df["Close"].iloc[-1])
    return s3, (price >= s3 * 0.98) and (price <= s3 * 1.02), price > s3


def finite(*values):
    return all(isinstance(value, (int, float)) and math.isfinite(value) for value in values)


def clean_df(df):
    if df is None or df.empty:
        return None
    df = df.copy()
    df.columns = [str(col).strip().title() for col in df.columns]
    for column in ["Open", "High", "Low", "Close", "Volume"]:
        if column not in df.columns:
            return None
        df[column] = pd.to_numeric(df[column], errors="coerce").astype(float)
    df.dropna(subset=["Open", "High", "Low", "Close", "Volume"], inplace=True)
    return df if len(df) > 0 else None


def extract_ticker(raw, yahoo_symbol):
    if not isinstance(raw.columns, pd.MultiIndex):
        return clean_df(raw)
    level0 = raw.columns.get_level_values(0)
    level1 = raw.columns.get_level_values(1)
    try:
        if yahoo_symbol in level0:
            return clean_df(raw[yahoo_symbol])
        if yahoo_symbol in level1:
            return clean_df(raw.xs(yahoo_symbol, axis=1, level=1))
    except Exception:
        return None
    return None


def download_batch(symbols, period, interval, auto_adjust=None, max_retries=3, min_coverage=0.60, yf_threads=False):
    yahoo_symbols = [f"{symbol}.IS" for symbol in symbols]
    kwargs = {
        "tickers": yahoo_symbols,
        "period": period,
        "interval": interval,
        "progress": False,
        "ignore_tz": True,
        "group_by": "ticker",
        "threads": yf_threads,
    }
    if auto_adjust is not None:
        kwargs["auto_adjust"] = auto_adjust

    best_result = {}
    for attempt in range(max_retries):
        try:
            raw = yf.download(**kwargs)
        except Exception:
            if attempt == max_retries - 1:
                return best_result
            time.sleep(1)
            continue

        if raw is None or raw.empty:
            if attempt < max_retries - 1:
                time.sleep(1 + attempt)
            continue

        result = {}
        for symbol, yahoo_symbol in zip(symbols, yahoo_symbols):
            df = extract_ticker(raw, yahoo_symbol)
            if df is not None:
                result[symbol] = df
        if len(result) > len(best_result):
            best_result = result
        if len(result) >= max(1, int(len(symbols) * min_coverage)):
            return result
        if attempt < max_retries - 1:
            time.sleep(1 + attempt)

    return best_result


def intraday_metrics(df, poc_lookback):
    close = df["Close"]
    volume = df["Volume"]
    poc, vah = volume_profile(df, lookback=poc_lookback)
    avg_volume = volume.iloc[-21:-1].mean()
    volume_x = float(volume.iloc[-1] / avg_volume) if avg_volume > 0 else 0.0
    metrics = {
        "price": float(close.iloc[-1]),
        "s20": float(sma(close, 20).iloc[-1]),
        "s50": float(sma(close, 50).iloc[-1]),
        "s200": float(sma(close, 200).iloc[-1]),
        "rsi": float(rsi(close).iloc[-1]),
        "vwap": float(intraday_vwap(df).iloc[-1]),
        "poc": poc,
        "vah": vah,
        "atr": float(atr(df).iloc[-1]),
        "trp": trp_buy_signal(close),
        "obv": obv_signal(df),
        "volume_x": volume_x,
    }
    if not finite(
        metrics["price"],
        metrics["s20"],
        metrics["s50"],
        metrics["s200"],
        metrics["rsi"],
        metrics["vwap"],
        metrics["poc"],
        metrics["vah"],
        metrics["atr"],
        metrics["volume_x"],
    ):
        return None
    return metrics


def analyze_intraday(symbol, data_1h, data_15m):
    if data_1h is None or data_15m is None:
        return None
    if len(data_1h) < 200 or len(data_15m) < 200:
        return None

    m1 = intraday_metrics(data_1h, 80)
    m15 = intraday_metrics(data_15m, 40)
    if m1 is None or m15 is None:
        return None

    price = m1["price"]
    score = 0
    details = []

    if price > m1["vwap"] and price > m1["poc"]:
        score += 2
    if price > m15["vwap"] and price > m15["poc"]:
        score += 2
    if price > m1["s20"] and m1["s20"] > m1["s50"]:
        score += 1
        details.append("1h boga trendi")
    if price < m1["s200"]:
        score -= 2
        details.append("1h SMA200 alti")
    if m1["trp"] or m15["trp"]:
        score += 1
        details.append("TRP(9) AL")
    if m1["obv"]:
        score += 1
        details.append("1h OBV")
    if m15["obv"]:
        score += 1
        details.append("15m OBV")
    if price > m1["vah"]:
        score += 1
        details.append("1h VAH kirilimi")
    if m1["volume_x"] > 1.5 or m15["volume_x"] > 1.5:
        score += 1
        details.append("hacim sicramasi")

    return {
        "symbol": symbol,
        "score": int(score),
        "price": round(price, 2),
        "stop_loss": round(price - (1.5 * m1["atr"]), 2),
        "rsi_1h": round(m1["rsi"], 1),
        "rsi_15m": round(m15["rsi"], 1),
        "volume_x_1h": round(m1["volume_x"], 1),
        "volume_x_15m": round(m15["volume_x"], 1),
        "vwap_1h": "Ust" if price > m1["vwap"] else "Alt",
        "poc_1h": "Ust" if price > m1["poc"] else "Alt",
        "vwap_15m": "Ust" if price > m15["vwap"] else "Alt",
        "poc_15m": "Ust" if price > m15["poc"] else "Alt",
        "details": details,
    }


def daily_metrics(df, now):
    close = df["Close"]
    volume = df["Volume"]
    price = float(close.iloc[-1])
    projected_volume = float(volume.iloc[-1])
    if 10 <= now.hour < 18:
        elapsed = (now.hour - 10) * 60 + now.minute
        projected_volume = projected_volume * (480 / max(elapsed, 1))
    avg_volume = volume.iloc[-21:-1].mean()
    volume_x = float(projected_volume / avg_volume) if avg_volume > 0 else 0.0
    poc, _ = volume_profile(df, lookback=22)
    s3, near_s3, above_s3 = pivot_s3_status(df)
    metrics = {
        "price": price,
        "s20": float(sma(close, 20).iloc[-1]),
        "s50": float(sma(close, 50).iloc[-1]),
        "s200": float(sma(close, 200).iloc[-1]),
        "rsi": float(rsi(close).iloc[-1]),
        "vwap": float(rolling_vwap(df, 20).iloc[-1]),
        "poc": poc,
        "atr": float(atr(df).iloc[-1]),
        "trp": trp_buy_signal(close),
        "obv": obv_signal(df),
        "near_s3": near_s3,
        "above_s3": above_s3,
        "volume_x": volume_x,
        "s3": s3,
    }
    if not finite(
        metrics["price"],
        metrics["s20"],
        metrics["s50"],
        metrics["s200"],
        metrics["rsi"],
        metrics["vwap"],
        metrics["poc"],
        metrics["atr"],
        metrics["volume_x"],
    ):
        return None
    return metrics


def analyze_daily(symbol, df, now):
    if df is None or len(df) < 200:
        return None
    metrics = daily_metrics(df, now)
    if metrics is None:
        return None

    price = metrics["price"]
    score = 0
    details = []

    if price < metrics["s200"]:
        score -= 2
        details.append("ayi trendi")
    else:
        score += 1
        details.append("boga")
    if metrics["trp"]:
        score += 3
        details.append("TRP(9) AL")
    if metrics["obv"]:
        score += 2
        details.append("OBV kirilim")
    if metrics["near_s3"] and metrics["above_s3"]:
        score += 1
        details.append("S3 destek")
    if price > metrics["s20"] and metrics["s20"] > metrics["s50"]:
        score += 1
        details.append("kusursuz trend")
    if 50 < metrics["rsi"] < 70:
        score += 1
        details.append("ideal RSI")
    if metrics["volume_x"] > 1.5:
        score += 1
        details.append(f"HacimX{metrics['volume_x']:.1f}")

    return {
        "symbol": symbol,
        "score": int(score),
        "price": round(price, 2),
        "stop_loss": round(price - (1.5 * metrics["atr"]), 2),
        "rsi": round(metrics["rsi"], 1),
        "volume_x": round(metrics["volume_x"], 2),
        "approval": "GUVENLI" if (price > metrics["poc"] and price > metrics["vwap"]) else "RISKLI",
        "details": details,
    }


def chunks(values, size):
    for start in range(0, len(values), size):
        yield values[start : start + size]


def download_intraday(symbols, batch_size, workers, yf_threads):
    groups = list(chunks(symbols, batch_size))
    data_1h = {}
    data_15m = {}

    def download_group(group):
        return (
            download_batch(group, period="3mo", interval="1h", yf_threads=yf_threads),
            download_batch(group, period="1mo", interval="15m", yf_threads=yf_threads),
        )

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        for one_hour, fifteen_min in executor.map(download_group, groups):
            data_1h.update(one_hour)
            data_15m.update(fifteen_min)

    return data_1h, data_15m


def download_daily(symbols, batch_size, workers, yf_threads):
    groups = list(chunks(symbols, batch_size))
    data = {}

    def download_group(group):
        return download_batch(group, period="1y", interval="1d", auto_adjust=True, yf_threads=yf_threads)

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        for batch in executor.map(download_group, groups):
            data.update(batch)

    return data


def min_data_symbols(total, coverage):
    return max(1, int(total * coverage))


def scan_intraday(args, symbols, started_at, now):
    data_1h = {}
    data_15m = {}
    common_symbols = []
    minimum = min_data_symbols(len(symbols), args.min_data_coverage)
    for attempt in range(args.scan_retries):
        attempt_1h, attempt_15m = download_intraday(symbols, args.batch_size, args.workers, args.yf_threads)
        attempt_common = [symbol for symbol in symbols if symbol in attempt_1h and symbol in attempt_15m]
        if len(attempt_common) > len(common_symbols):
            data_1h = attempt_1h
            data_15m = attempt_15m
            common_symbols = attempt_common
        if len(attempt_common) >= minimum:
            break
        if attempt < args.scan_retries - 1:
            time.sleep(5 + (attempt * 5))

    candidates = []
    analyzed = 0

    max_workers = min(32, (os.cpu_count() or 4) * 4)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(analyze_intraday, symbol, data_1h.get(symbol), data_15m.get(symbol)): symbol
            for symbol in common_symbols
        }
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            if result is None:
                continue
            analyzed += 1
            if (
                result["score"] >= args.min_score
                and result["rsi_1h"] < 80
                and result["rsi_15m"] < 80
            ):
                candidates.append(result)

    candidates.sort(key=lambda item: (-item["score"], item["rsi_1h"]))
    return build_report(
        args=args,
        symbols=symbols,
        started_at=started_at,
        results=candidates[: args.max_results],
        data_symbols=len(common_symbols),
        analyzed_symbols=analyzed,
        source="Python yfinance batch scanner",
        interval_summary="3mo / 1h + 1mo / 15m",
        filter_summary=f"Skor >= {args.min_score}, 1h RSI < 80, 15m RSI < 80",
    )


def scan_daily(args, symbols, started_at, now):
    data = {}
    minimum = min_data_symbols(len(symbols), args.min_data_coverage)
    for attempt in range(args.scan_retries):
        attempt_data = download_daily(symbols, args.batch_size, args.workers, args.yf_threads)
        if len(attempt_data) > len(data):
            data = attempt_data
        if len(attempt_data) >= minimum:
            break
        if attempt < args.scan_retries - 1:
            time.sleep(5 + (attempt * 5))

    candidates = []
    analyzed = 0

    max_workers = min(32, (os.cpu_count() or 4) * 4)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(analyze_daily, symbol, df, now): symbol
            for symbol, df in data.items()
        }
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            if result is None:
                continue
            analyzed += 1
            if result["score"] >= args.min_score and result["rsi"] < 80:
                candidates.append(result)

    candidates.sort(key=lambda item: (-item["score"], -item["volume_x"], item["rsi"]))
    return build_report(
        args=args,
        symbols=symbols,
        started_at=started_at,
        results=candidates[: args.max_results],
        data_symbols=len(data),
        analyzed_symbols=analyzed,
        source="Python yfinance batch scanner",
        interval_summary="1y / 1d",
        filter_summary=f"Skor >= {args.min_score}, RSI < 80",
    )


def build_report(args, symbols, started_at, results, data_symbols, analyzed_symbols, source, interval_summary, filter_summary):
    finished_at = datetime.now(started_at.tzinfo)
    return {
        "mode": args.mode,
        "universe_key": args.universe_key,
        "universe_name": args.universe_name,
        "started_at": started_at.isoformat(),
        "finished_at": finished_at.isoformat(),
        "total_symbols": len(symbols),
        "data_symbols": data_symbols,
        "analyzed_symbols": analyzed_symbols,
        "failed_symbols": max(len(symbols) - data_symbols, 0),
        "min_score": args.min_score,
        "max_results": args.max_results,
        "results": results,
        "source": source,
        "interval_summary": interval_summary,
        "filter_summary": filter_summary,
        "error_samples": [],
    }


def run_scan(args):
    try:
        timezone = ZoneInfo(args.timezone)
    except Exception:
        timezone = ZoneInfo("Europe/Istanbul")
    started_at = datetime.now(timezone)
    now = started_at
    symbols = read_symbols(args.symbols_file)
    if args.mode == "gunici":
        return scan_intraday(args, symbols, started_at, now)
    return scan_daily(args, symbols, started_at, now)


def print_terminal_report(report):
    if report["mode"] == "gunici":
        print_intraday_terminal_report(report)
        return
    print_daily_terminal_report(report)


def print_intraday_terminal_report(report):
    print()
    print("=" * 140)
    print(f"  BIST CIFT ZAMAN DILIMI (1h & 15m) PROFESYONEL TARAMA - {report['finished_at'][:16].replace('T', ' ')}")
    print(f"  Kapsam: {report['universe_name']} | Veri: {report['data_symbols']}/{report['total_symbols']} | Analiz: {report['analyzed_symbols']}")
    print("=" * 140)
    print()
    if not report["results"]:
        print("  Kriterlere uyan hisse bulunamadi.")
        print()
        return
    header = (
        f"  {'HISSE':<6} | {'SKOR':<5} | {'FIYAT':<8} | {'1h VWAP':<7} | {'1h POC':<7} | "
        f"{'15m VWAP':<8} | {'15m POC':<7} | {'1h RSI':<6} | {'15m RSI':<7} | "
        f"{'1h Hacim':<8} | {'15m Hacim':<9} | SINYALLER"
    )
    print(header)
    print("  " + "-" * 138)
    for item in report["results"]:
        print(
            f"  {item['symbol']:<6} | {item['score']:>2}/10 | {item['price']:>8.2f} | "
            f"{terminal_position(item.get('vwap_1h')):<7} | {terminal_position(item.get('poc_1h')):<7} | "
            f"{terminal_position(item.get('vwap_15m')):<8} | {terminal_position(item.get('poc_15m')):<7} | "
            f"{item.get('rsi_1h', 0):>6.1f} | {item.get('rsi_15m', 0):>7.1f} | "
            f"x{item.get('volume_x_1h', 0):<7.1f} | x{item.get('volume_x_15m', 0):<8.1f} | "
            f"{' | '.join(item.get('details', []))}"
        )
    print()
    print("=" * 140)
    print()


def print_daily_terminal_report(report):
    print()
    print("=" * 130)
    print(f"  BIST GUCLU AL SINYALI TARAMASI - {report['finished_at'][:16].replace('T', ' ')}")
    print(f"  Kapsam: {report['universe_name']} | Veri: {report['data_symbols']}/{report['total_symbols']} | Analiz: {report['analyzed_symbols']}")
    print("=" * 130)
    print()
    if not report["results"]:
        print("  Kriterlere uyan hisse bulunamadi.")
        print()
        return
    header = f"  {'HISSE':<6} | {'SKOR':<5} | {'FIYAT':<8} | {'RSI':<5} | {'HACIM':<6} | {'ONAY':<8} | SINYALLER"
    print(header)
    print("  " + "-" * 128)
    for item in report["results"]:
        print(
            f"  {item['symbol']:<6} | {item['score']:>2}/10 | {item['price']:>8.2f} | "
            f"{item.get('rsi', 0):>5.1f} | x{item.get('volume_x', 0):<5.1f} | "
            f"{terminal_approval(item.get('approval')):<8} | {' | '.join(item.get('details', []))}"
        )
    print()


def terminal_position(value):
    return "Ust" if value == "Ust" else "Alt"


def terminal_approval(value):
    return "GUVENLI" if value == "GUVENLI" else "RISKLI"


def legacy_args(mode, symbols_file, min_score, universe_name="BIST Tum", max_results=10):
    return SimpleNamespace(
        mode=mode,
        symbols_file=symbols_file,
        universe_key="tum",
        universe_name=universe_name,
        min_score=min_score,
        max_results=max_results,
        timezone=os.getenv("MARKET_TIMEZONE", "Europe/Istanbul"),
        batch_size=int(os.getenv("PYTHON_SCANNER_BATCH_SIZE", "50")),
        workers=int(os.getenv("PYTHON_SCANNER_WORKERS", "1")),
        scan_retries=int(os.getenv("PYTHON_SCANNER_SCAN_RETRIES", "3")),
        min_data_coverage=float(os.getenv("PYTHON_SCANNER_MIN_DATA_COVERAGE", "0.50")),
        yf_threads=os.getenv("PYTHON_SCANNER_YF_THREADS", "false").lower() in {"1", "true", "yes", "on"},
    )


def parse_args():
    parser = argparse.ArgumentParser(description="BIST scanner JSON bridge")
    parser.add_argument("--mode", choices=["gunici", "gunluk"], required=True)
    parser.add_argument("--symbols-file", required=True)
    parser.add_argument("--universe-key", required=True)
    parser.add_argument("--universe-name", required=True)
    parser.add_argument("--min-score", type=int, required=True)
    parser.add_argument("--max-results", type=int, default=10)
    parser.add_argument("--timezone", default="Europe/Istanbul")
    parser.add_argument("--batch-size", type=int, default=50)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--scan-retries", type=int, default=3)
    parser.add_argument("--min-data-coverage", type=float, default=0.50)
    parser.add_argument("--yf-threads", action="store_true")
    parser.add_argument("--output", choices=["json", "text"], default="json")
    return parser.parse_args()


def main():
    args = parse_args()
    report = run_scan(args)
    if args.output == "text":
        print_terminal_report(report)
    else:
        json.dump(report, sys.stdout, ensure_ascii=True, allow_nan=False)
        sys.stdout.write("\n")


if __name__ == "__main__":
    main()
