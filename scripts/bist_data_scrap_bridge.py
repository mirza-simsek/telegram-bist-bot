#!/usr/bin/env python3
"""JSON bridge between the Go bot and the shared BIST30 analysis engine."""

from __future__ import annotations

import argparse
import concurrent.futures
from datetime import datetime
import json
import logging
import sys
from typing import Any
from zoneinfo import ZoneInfo

import pandas as pd
import yfinance as yf

from analysis_engine import analyze_daily_pair, analyze_intraday_tl, json_safe

logging.getLogger("yfinance").setLevel(logging.CRITICAL)


def read_symbols(path: str) -> list[str]:
    with open(path, "r", encoding="utf-8") as handle:
        result = []
        for line in handle:
            symbol = line.strip().upper()
            if symbol and not symbol.startswith("#") and symbol not in result:
                result.append(symbol)
        return result


def normalize_frame(frame: pd.DataFrame | None) -> pd.DataFrame | None:
    if frame is None or frame.empty:
        return None
    result = frame.copy()
    if isinstance(result.columns, pd.MultiIndex):
        known = {"Open", "High", "Low", "Close", "Adj Close", "Volume"}
        level = 0 if any(str(item) in known for item in result.columns.get_level_values(0)) else 1
        result.columns = result.columns.get_level_values(level)
    return result.loc[:, ~result.columns.duplicated()].copy()


def download(symbol: str, period: str, interval: str) -> pd.DataFrame | None:
    ticker = symbol if symbol.endswith("=X") else f"{symbol}.IS"
    try:
        frame = yf.download(
            ticker, period=period, interval=interval, auto_adjust=False,
            progress=False, threads=False, timeout=20,
        )
        return normalize_frame(frame)
    except Exception as exc:
        logging.warning("download failed for %s %s: %s", ticker, interval, exc)
        return None


def download_intraday(symbol: str) -> tuple[pd.DataFrame | None, pd.DataFrame | None]:
    # Yahoo accepts 5-minute history most reliably with the explicit 5d window.
    return download(symbol, "5d", "5m"), download(symbol, "1mo", "15m")


def download_daily(symbol: str) -> pd.DataFrame | None:
    return download(symbol, "3y", "1d")


def metric(result: dict[str, Any], key: str, default: Any = 0) -> Any:
    return result.get("metrics", {}).get(key, default)


def intraday_signal(symbol: str, result: dict[str, Any]) -> dict[str, Any]:
    price = float(result.get("price", 0) or 0)
    vwap_5m = float(metric(result, "vwap_5m", 0) or 0)
    vwap_15m = float(metric(result, "vwap_15m", 0) or 0)
    return {
        "symbol": symbol,
        "score": result["score"], "tl_score": result["score"],
        "tl_status": result["status"], "price": price,
        "stop_loss": metric(result, "stop_loss"),
        "rsi_5m": metric(result, "rsi_5m"), "rsi_15m": metric(result, "rsi_15m"),
        "volume_x_5m": metric(result, "volume_x_5m"),
        "volume_x_15m": metric(result, "volume_x_15m"),
        "vwap_5m": "Ust" if price >= vwap_5m > 0 else "Alt",
        "vwap_15m": "Ust" if price >= vwap_15m > 0 else "Alt",
        "details": result.get("details", []), "tl_details": result.get("details", []),
        "metrics": result.get("metrics", {}),
    }


def daily_signal(symbol: str, tl_result: dict[str, Any], usd_result: dict[str, Any] | None, metadata: dict[str, Any]) -> dict[str, Any]:
    return {
        "symbol": symbol, "score": tl_result["score"],
        "tl_score": tl_result["score"], "tl_status": tl_result["status"],
        "usd_score": usd_result["score"] if usd_result else 0,
        "usd_status": usd_result["status"] if usd_result else "veri_yok",
        "usd_price": usd_result["price"] if usd_result else 0,
        "usd_data_quality": metadata.get("usd_data_quality", "missing"),
        "price": tl_result["price"], "stop_loss": metric(tl_result, "support"),
        "rsi": metric(tl_result, "rsi"), "volume_x": metric(tl_result, "volume_x"),
        "approval": "GUVENLI" if tl_result["status"] == "AL" else "RISKLI",
        "details": tl_result.get("details", []), "tl_details": tl_result.get("details", []),
        "usd_details": usd_result.get("details", []) if usd_result else [],
        "metrics": tl_result.get("metrics", {}),
        "usd_metrics": usd_result.get("metrics", {}) if usd_result else {},
    }


def scan_intraday(args: argparse.Namespace, symbols: list[str]) -> tuple[list[dict[str, Any]], int, int, list[str]]:
    candidates, errors = [], []
    data_symbols = analyzed = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.workers)) as executor:
        futures = {executor.submit(download_intraday, symbol): symbol for symbol in symbols}
        for future in concurrent.futures.as_completed(futures):
            symbol = futures[future]
            try:
                frame_5m, frame_15m = future.result()
                if frame_5m is None or frame_15m is None:
                    errors.append(f"{symbol}: eksik 5dk/15dk veri")
                    continue
                data_symbols += 1
                score = analyze_intraday_tl(frame_5m, frame_15m)
                if score is None:
                    errors.append(f"{symbol}: yetersiz tamamlanmis mum")
                    continue
                analyzed += 1
                result = score.as_dict()
                if result["score"] >= args.min_score and result["status"] != "risk":
                    candidates.append(intraday_signal(symbol, result))
            except Exception as exc:
                errors.append(f"{symbol}: {type(exc).__name__}: {exc}")
    candidates.sort(key=lambda item: (-item["tl_score"], item["rsi_5m"], item["symbol"]))
    return candidates[:args.max_results], data_symbols, analyzed, errors[:8]


def scan_daily(args: argparse.Namespace, symbols: list[str]) -> tuple[list[dict[str, Any]], int, int, list[str]]:
    candidates, errors = [], []
    data_symbols = analyzed = 0
    fx = download("TRY=X", "3y", "1d")
    if fx is None:
        errors.append("USDTRY: veri indirilemedi; USD puanlari veri_yok")
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.workers)) as executor:
        futures = {executor.submit(download_daily, symbol): symbol for symbol in symbols}
        for future in concurrent.futures.as_completed(futures):
            symbol = futures[future]
            try:
                frame = future.result()
                if frame is None:
                    errors.append(f"{symbol}: gunluk veri indirilemedi")
                    continue
                data_symbols += 1
                tl_score, usd_score, metadata = analyze_daily_pair(frame, fx)
                if tl_score is None:
                    errors.append(f"{symbol}: yetersiz tamamlanmis gunluk mum")
                    continue
                analyzed += 1
                tl_result = tl_score.as_dict()
                usd_result = usd_score.as_dict() if usd_score else None
                if tl_result["score"] >= args.min_score or (usd_result and usd_result["score"] >= args.min_score):
                    candidates.append(daily_signal(symbol, tl_result, usd_result, metadata))
            except Exception as exc:
                errors.append(f"{symbol}: {type(exc).__name__}: {exc}")
    candidates.sort(key=lambda item: (-max(item["tl_score"], item["usd_score"]), -item["tl_score"], item["symbol"]))
    return candidates[:args.max_results], data_symbols, analyzed, errors[:8]


def build_card(args: argparse.Namespace) -> dict[str, Any]:
    symbol = args.symbol.strip().upper()
    frame_5m, frame_15m = download_intraday(symbol)
    daily, fx = download_daily(symbol), download("TRY=X", "3y", "1d")
    intraday = analyze_intraday_tl(frame_5m, frame_15m) if frame_5m is not None and frame_15m is not None else None
    tl_daily, usd_daily, metadata = analyze_daily_pair(daily, fx) if daily is not None else (None, None, {"usd_data_quality": "missing"})
    return json_safe({
        "symbol": symbol, "finished_at": datetime.now(ZoneInfo(args.timezone)),
        "source": "Yahoo Finance + BIST30 ortak analiz motoru",
        "intraday_tl": intraday.as_dict() if intraday else None,
        "daily_tl": tl_daily.as_dict() if tl_daily else None,
        "daily_usd": usd_daily.as_dict() if usd_daily else None,
        "fx": metadata,
    })


def build_report(args: argparse.Namespace, symbols: list[str], started_at: datetime, results: list[dict[str, Any]], data_symbols: int, analyzed: int, errors: list[str]) -> dict[str, Any]:
    daily = args.mode == "gunluk"
    return json_safe({
        "mode": args.mode, "universe_key": args.universe_key, "universe_name": args.universe_name,
        "started_at": started_at, "finished_at": datetime.now(started_at.tzinfo),
        "total_symbols": len(symbols), "data_symbols": data_symbols,
        "analyzed_symbols": analyzed, "failed_symbols": max(len(symbols) - data_symbols, 0),
        "min_score": args.min_score, "max_results": args.max_results, "results": results,
        "source": "Yahoo Finance + BIST30 ortak analiz motoru",
        "interval_summary": "3y / 1d TL + USDTRY ile USD" if daily else "5d / 5dk TL + 1mo / 15dk TL",
        "filter_summary": f"TL veya USD skor >= {args.min_score}; puanlar ayri" if daily else f"TL skor >= {args.min_score}; yalniz TL",
        "error_samples": errors,
    })


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="BIST30 shared analysis engine JSON bridge")
    parser.add_argument("--mode", choices=["gunici", "gunluk", "card"], required=True)
    parser.add_argument("--symbols-file")
    parser.add_argument("--symbol")
    parser.add_argument("--universe-key", default="bist30")
    parser.add_argument("--universe-name", default="BIST 30")
    parser.add_argument("--min-score", type=int, default=3)
    parser.add_argument("--max-results", type=int, default=10)
    parser.add_argument("--timezone", default="Europe/Istanbul")
    parser.add_argument("--batch-size", type=int, default=50)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--scan-retries", type=int, default=1)
    parser.add_argument("--min-data-coverage", type=float, default=0.50)
    parser.add_argument("--yf-threads", action="store_true")
    args = parser.parse_args()
    if args.mode == "card" and not args.symbol:
        parser.error("--symbol is required for card mode")
    if args.mode != "card" and not args.symbols_file:
        parser.error("--symbols-file is required for scans")
    return args


def main() -> None:
    args = parse_args()
    if args.mode == "card":
        payload = build_card(args)
    else:
        timezone = ZoneInfo(args.timezone)
        started_at = datetime.now(timezone)
        symbols = read_symbols(args.symbols_file)
        if args.mode == "gunici":
            results, data_symbols, analyzed, errors = scan_intraday(args, symbols)
        else:
            results, data_symbols, analyzed, errors = scan_daily(args, symbols)
        payload = build_report(args, symbols, started_at, results, data_symbols, analyzed, errors)
    json.dump(payload, sys.stdout, ensure_ascii=True, allow_nan=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
