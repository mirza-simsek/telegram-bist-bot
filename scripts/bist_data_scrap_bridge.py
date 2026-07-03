#!/usr/bin/env python3
import argparse
import concurrent.futures
import contextlib
import importlib.util
import json
import math
import os
import sys
import time
from datetime import datetime
from zoneinfo import ZoneInfo


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"module could not be loaded: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def bist_data_scrap_dir():
    default_dir = os.path.dirname(os.path.abspath(__file__))
    return os.getenv("BIST_DATA_SCRAP_DIR", default_dir)


def legacy_module(filename):
    path = os.path.join(bist_data_scrap_dir(), filename)
    if not os.path.exists(path):
        raise FileNotFoundError(f"bist_data_scrap file not found: {path}")
    module_name = os.path.splitext(filename)[0] + "_legacy"
    return load_module(module_name, path)


def read_legacy_symbols(module, symbols_file):
    with contextlib.redirect_stdout(sys.stderr):
        symbols = module.hisseleri_oku(symbols_file)
    return [symbol.strip().upper() for symbol in symbols if symbol and symbol.strip()]


def chunks(values, size):
    for start in range(0, len(values), size):
        yield values[start : start + size]


def split_details(value):
    if not value:
        return []
    return [part.strip() for part in str(value).split("|") if part.strip()]


def position(value):
    text = str(value or "").lower()
    if "üst" in text or "ust" in text:
        return "Ust"
    return "Alt"


def approval(value):
    text = str(value or "").lower()
    if "güven" in text or "guven" in text:
        return "GUVENLI"
    return "RISKLI"


def finite_number(value, fallback=0.0):
    try:
        number = float(value)
    except (TypeError, ValueError):
        return fallback
    if not math.isfinite(number):
        return fallback
    return number


def safe_int(value, fallback=0):
    try:
        return int(value)
    except (TypeError, ValueError):
        return fallback


def intraday_signal(item):
    return {
        "symbol": str(item.get("Hisse", "")).upper(),
        "score": safe_int(item.get("Skor")),
        "price": finite_number(item.get("Fiyat")),
        "stop_loss": finite_number(item.get("StopLoss")),
        "rsi_1h": finite_number(item.get("RSI_1h")),
        "rsi_15m": finite_number(item.get("RSI_15m")),
        "volume_x_1h": finite_number(item.get("HacimX_1h")),
        "volume_x_15m": finite_number(item.get("HacimX_15m")),
        "vwap_1h": position(item.get("VWAP_1h")),
        "poc_1h": position(item.get("POC_1h")),
        "vwap_15m": position(item.get("VWAP_15m")),
        "poc_15m": position(item.get("POC_15m")),
        "details": split_details(item.get("Sinyaller")),
    }


def daily_signal(item):
    return {
        "symbol": str(item.get("Hisse", "")).upper(),
        "score": safe_int(item.get("Skor")),
        "price": finite_number(item.get("Fiyat")),
        "stop_loss": finite_number(item.get("StopLoss")),
        "rsi": finite_number(item.get("RSI")),
        "volume_x": finite_number(item.get("HacimX")),
        "approval": approval(item.get("Onay")),
        "details": split_details(item.get("Sinyal")),
    }


def scan_intraday(args, symbols):
    module = legacy_module("gun_ici_tarama.py")
    batch_size = max(args.batch_size, 1)
    workers = max(args.workers, 4)
    groups = list(chunks(symbols, batch_size))
    data_1h = {}
    data_15m = {}

    def download_group(group):
        return (
            module.toplu_veri_indir(group, period="3mo", interval="1h"),
            module.toplu_veri_indir(group, period="1mo", interval="15m"),
        )

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        for one_hour, fifteen_min in executor.map(download_group, groups):
            data_1h.update(one_hour)
            data_15m.update(fifteen_min)

    common_symbols = [symbol for symbol in symbols if symbol in data_1h and symbol in data_15m]
    candidates = []
    analyzed = 0
    max_workers = min(32, (os.cpu_count() or 4) * 4)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(module.analiz_et, symbol, data_1h.get(symbol), data_15m.get(symbol)): symbol
            for symbol in common_symbols
        }
        for future in concurrent.futures.as_completed(futures):
            item = future.result()
            if item is None:
                continue
            analyzed += 1
            if item["Skor"] >= args.min_score and item["RSI_1h"] < 80 and item["RSI_15m"] < 80:
                candidates.append(intraday_signal(item))

    candidates.sort(key=lambda item: (-item["score"], item["rsi_1h"]))
    return candidates[: args.max_results], len(common_symbols), analyzed


def scan_daily(args, symbols):
    module = legacy_module("gunluk_tarama.py")
    batch_size = max(args.batch_size, 1)
    workers = max(args.workers, 4)
    groups = list(chunks(symbols, batch_size))
    data = {}

    def download_group(group):
        return module.toplu_veri_indir(group, period="1y", interval="1d")

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        for batch in executor.map(download_group, groups):
            data.update(batch)

    candidates = []
    analyzed = 0
    max_workers = min(32, (os.cpu_count() or 4) * 4)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {executor.submit(module.hisse_analiz, symbol, df): symbol for symbol, df in data.items()}
        for future in concurrent.futures.as_completed(futures):
            item = future.result()
            if item is None:
                continue
            analyzed += 1
            if item["Skor"] >= args.min_score and item["RSI"] < 80:
                candidates.append(daily_signal(item))

    candidates.sort(key=lambda item: (-item["score"], -item["volume_x"], item["rsi"]))
    return candidates[: args.max_results], len(data), analyzed


def build_report(args, symbols, started_at, results, data_symbols, analyzed_symbols):
    finished_at = datetime.now(started_at.tzinfo)
    if args.mode == "gunici":
        interval_summary = "bist_data_scrap: 3mo / 1h + 1mo / 15m"
        filter_summary = f"Skor >= {args.min_score}, 1h RSI < 80, 15m RSI < 80"
    else:
        interval_summary = "bist_data_scrap: 1y / 1d"
        filter_summary = f"Skor >= {args.min_score}, RSI < 80"

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
        "source": "bist_data_scrap legacy scanner",
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
    module = legacy_module("gun_ici_tarama.py" if args.mode == "gunici" else "gunluk_tarama.py")
    symbols = read_legacy_symbols(module, args.symbols_file)

    if args.mode == "gunici":
        results, data_symbols, analyzed_symbols = scan_intraday(args, symbols)
    else:
        results, data_symbols, analyzed_symbols = scan_daily(args, symbols)

    return build_report(args, symbols, started_at, results, data_symbols, analyzed_symbols)


def parse_args():
    parser = argparse.ArgumentParser(description="BIST data scrap legacy scanner JSON bridge")
    parser.add_argument("--mode", choices=["gunici", "gunluk"], required=True)
    parser.add_argument("--symbols-file", required=True)
    parser.add_argument("--universe-key", required=True)
    parser.add_argument("--universe-name", required=True)
    parser.add_argument("--min-score", type=int, required=True)
    parser.add_argument("--max-results", type=int, default=10)
    parser.add_argument("--timezone", default="Europe/Istanbul")
    parser.add_argument("--batch-size", type=int, default=50)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--scan-retries", type=int, default=1)
    parser.add_argument("--min-data-coverage", type=float, default=0.50)
    parser.add_argument("--yf-threads", action="store_true")
    return parser.parse_args()


def main():
    args = parse_args()
    report = run_scan(args)
    json.dump(report, sys.stdout, ensure_ascii=True, allow_nan=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
