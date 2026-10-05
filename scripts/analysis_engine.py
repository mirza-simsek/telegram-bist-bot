"""BIST30 technical-analysis primitives shared by scans and symbol cards.

All decisions use completed bars. Intraday analysis is TRY-only. Daily analysis
returns independent TRY and USD scores; callers must not combine the scores.
The proprietary TradingView range-filter source is unavailable; the dual filter
below is a candidate reconstruction, not a claim of exact parity.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, time as clock_time
import math
from typing import Any
from zoneinfo import ZoneInfo

import numpy as np
import pandas as pd


ISTANBUL = ZoneInfo("Europe/Istanbul")
RULE_VERSION = "bist30-v1"


@dataclass
class ScoreResult:
    score: int
    status: str
    price: float
    details: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    metrics: dict[str, Any] = field(default_factory=dict)

    def as_dict(self) -> dict[str, Any]:
        return {
            "score": int(max(0, min(10, self.score))),
            "status": self.status,
            "price": rounded(self.price),
            "details": self.details,
            "warnings": self.warnings,
            "metrics": json_safe(self.metrics),
            "rule_version": RULE_VERSION,
        }


def rounded(value: Any, digits: int = 4) -> float:
    try:
        number = float(value)
    except (TypeError, ValueError):
        return 0.0
    return round(number, digits) if math.isfinite(number) else 0.0


def json_safe(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: json_safe(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [json_safe(item) for item in value]
    if isinstance(value, (np.bool_, bool)):
        return bool(value)
    if isinstance(value, (np.integer, int)):
        return int(value)
    if isinstance(value, (np.floating, float)):
        return rounded(value)
    if isinstance(value, (pd.Timestamp, datetime)):
        return value.isoformat()
    return value


def clean_ohlcv(frame: pd.DataFrame | None) -> pd.DataFrame | None:
    if frame is None or frame.empty:
        return None
    result = frame.copy()
    result.columns = [str(column).strip().title() for column in result.columns]
    required = ["Open", "High", "Low", "Close", "Volume"]
    if any(column not in result.columns for column in required):
        return None
    for column in required:
        result[column] = pd.to_numeric(result[column], errors="coerce")
    result = result.replace([np.inf, -np.inf], np.nan)
    result.dropna(subset=required, inplace=True)
    result = result[~result.index.duplicated(keep="last")].sort_index()
    return result if not result.empty else None


def completed_bars(
    frame: pd.DataFrame | None,
    interval: str,
    now: datetime | None = None,
) -> pd.DataFrame | None:
    result = clean_ohlcv(frame)
    if result is None:
        return None
    now = now or datetime.now(ISTANBUL)
    if now.tzinfo is None:
        now = now.replace(tzinfo=ISTANBUL)
    else:
        now = now.astimezone(ISTANBUL)

    index = pd.DatetimeIndex(result.index)
    if index.tz is None:
        localized = index.tz_localize(ISTANBUL, ambiguous="NaT", nonexistent="shift_forward")
    else:
        localized = index.tz_convert(ISTANBUL)
    valid_index = ~localized.isna()
    result = result.loc[valid_index].copy()
    localized = localized[valid_index]

    if interval == "1d":
        today = now.date()
        session_finished = now.time() >= clock_time(18, 10)
        keep = np.array([(stamp.date() < today) or (stamp.date() == today and session_finished) for stamp in localized])
    else:
        minutes = {"5m": 5, "15m": 15, "1h": 60}.get(interval)
        if minutes is None:
            raise ValueError(f"unsupported interval: {interval}")
        keep = (localized + pd.Timedelta(minutes=minutes)) <= pd.Timestamp(now)
    result = result.loc[np.asarray(keep)].copy()
    return result if not result.empty else None


def ema(series: pd.Series, period: int) -> pd.Series:
    return series.ewm(span=period, adjust=False, min_periods=period).mean()


def sma(series: pd.Series, period: int) -> pd.Series:
    return series.rolling(period, min_periods=period).mean()


def rsi(series: pd.Series, period: int = 14) -> pd.Series:
    delta = series.diff()
    gain = delta.clip(lower=0)
    loss = -delta.clip(upper=0)
    avg_gain = gain.ewm(alpha=1 / period, adjust=False, min_periods=period).mean()
    avg_loss = loss.ewm(alpha=1 / period, adjust=False, min_periods=period).mean()
    relative = avg_gain / avg_loss.replace(0, np.nan)
    output = 100 - (100 / (1 + relative))
    return output.where(avg_loss != 0, 100.0)


def macd(series: pd.Series, fast: int = 12, slow: int = 26, signal: int = 9) -> tuple[pd.Series, pd.Series, pd.Series]:
    line = ema(series, fast) - ema(series, slow)
    signal_line = ema(line, signal)
    return line, signal_line, line - signal_line


def atr(frame: pd.DataFrame, period: int = 14) -> pd.Series:
    previous = frame["Close"].shift(1)
    true_range = pd.concat(
        [
            frame["High"] - frame["Low"],
            (frame["High"] - previous).abs(),
            (frame["Low"] - previous).abs(),
        ],
        axis=1,
    ).max(axis=1)
    return true_range.rolling(period, min_periods=period).mean()


def cmf(frame: pd.DataFrame, period: int = 20) -> pd.Series:
    spread = (frame["High"] - frame["Low"]).replace(0, np.nan)
    multiplier = ((frame["Close"] - frame["Low"]) - (frame["High"] - frame["Close"])) / spread
    money_flow = multiplier.fillna(0) * frame["Volume"]
    volume = frame["Volume"].rolling(period, min_periods=period).sum().replace(0, np.nan)
    return money_flow.rolling(period, min_periods=period).sum() / volume


def obv(frame: pd.DataFrame) -> pd.Series:
    direction = np.sign(frame["Close"].diff()).fillna(0)
    return (direction * frame["Volume"]).cumsum()


def relative_volume(frame: pd.DataFrame, period: int = 20) -> pd.Series:
    baseline = frame["Volume"].shift(1).rolling(period, min_periods=period).mean()
    return frame["Volume"] / baseline.replace(0, np.nan)


def intraday_vwap(frame: pd.DataFrame) -> pd.Series:
    typical = (frame["High"] + frame["Low"] + frame["Close"]) / 3
    dates = pd.DatetimeIndex(frame.index).date
    cumulative_volume = frame["Volume"].groupby(dates).cumsum().replace(0, np.nan)
    cumulative_value = (typical * frame["Volume"]).groupby(dates).cumsum()
    return cumulative_value / cumulative_volume


def rolling_vwap(frame: pd.DataFrame, period: int = 20) -> pd.Series:
    typical = (frame["High"] + frame["Low"] + frame["Close"]) / 3
    volume = frame["Volume"].rolling(period, min_periods=period).sum().replace(0, np.nan)
    return (typical * frame["Volume"]).rolling(period, min_periods=period).sum() / volume


def range_filter(source: pd.Series, period: int, multiplier: float) -> pd.Series:
    average_range = source.diff().abs().ewm(span=period, adjust=False, min_periods=period).mean()
    smooth_range = average_range.ewm(span=(period * 2) - 1, adjust=False, min_periods=period).mean() * multiplier
    values = source.to_numpy(dtype=float)
    ranges = smooth_range.to_numpy(dtype=float)
    output = np.full(len(source), np.nan)
    for index, value in enumerate(values):
        if not math.isfinite(value):
            continue
        if index == 0 or not math.isfinite(output[index - 1]) or not math.isfinite(ranges[index]):
            output[index] = value
            continue
        previous = output[index - 1]
        if value > previous:
            output[index] = max(value - ranges[index], previous)
        else:
            output[index] = min(value + ranges[index], previous)
    return pd.Series(output, index=source.index, name="range_filter")


def dual_range_state(source: pd.Series) -> tuple[pd.Series, pd.Series, pd.Series]:
    fast = range_filter(source, 27, 1.6)
    slow = range_filter(source, 55, 2.0)
    line = (fast + slow) / 2
    state = pd.Series(np.where(source >= line, 1, -1), index=source.index, dtype=int)
    flip = state.ne(state.shift(1)).fillna(False)
    return line, state, flip


def confirmed_pivots(series: pd.Series, left: int = 3, right: int = 3, mode: str = "low") -> list[tuple[int, float]]:
    values = series.to_numpy(dtype=float)
    pivots: list[tuple[int, float]] = []
    for index in range(left, len(values) - right):
        value = values[index]
        if not math.isfinite(value):
            continue
        window = values[index - left : index + right + 1]
        if mode == "low" and value == np.nanmin(window) and np.count_nonzero(window == value) == 1:
            pivots.append((index, value))
        elif mode == "high" and value == np.nanmax(window) and np.count_nonzero(window == value) == 1:
            pivots.append((index, value))
    return pivots


def cluster_levels(
    pivots: list[tuple[int, float]],
    current_price: float,
    tolerance_pct: float = 0.004,
    min_touches: int = 2,
    max_distance_pct: float = 0.15,
) -> list[dict[str, Any]]:
    clusters: list[dict[str, Any]] = []
    for index, price in pivots[-60:]:
        if current_price <= 0 or abs(price - current_price) / current_price > max_distance_pct:
            continue
        match = next(
            (
                cluster
                for cluster in clusters
                if abs(price - cluster["price"]) / max(abs(cluster["price"]), 1e-9) <= tolerance_pct
            ),
            None,
        )
        if match is None:
            clusters.append({"price": price, "touches": 1, "last_index": index, "prices": [price]})
        else:
            match["prices"].append(price)
            match["touches"] += 1
            match["last_index"] = max(match["last_index"], index)
            match["price"] = float(np.mean(match["prices"]))
    result = [cluster for cluster in clusters if cluster["touches"] >= min_touches]
    result.sort(key=lambda cluster: (-cluster["touches"], -cluster["last_index"]))
    return result


def level_map(frame: pd.DataFrame, close_only: bool = False) -> dict[str, Any]:
    current = float(frame["Close"].iloc[-1])
    low_series = frame["Close"] if close_only else frame["Low"]
    high_series = frame["Close"] if close_only else frame["High"]
    lows = confirmed_pivots(low_series, mode="low")
    highs = confirmed_pivots(high_series, mode="high")
    support_clusters = cluster_levels(lows, current)
    resistance_clusters = cluster_levels(highs, current)
    supports = sorted((item for item in support_clusters if item["price"] <= current), key=lambda item: item["price"], reverse=True)[:3]
    resistances = sorted((item for item in resistance_clusters if item["price"] >= current), key=lambda item: item["price"])[:3]

    major_lows = sorted({rounded(price) for _, price in lows if price <= current and (current - price) / current <= 0.20}, reverse=True)[:5]
    major_highs = sorted({rounded(price) for _, price in highs if price >= current and (price - current) / current <= 0.20})[:5]
    return {
        "support": rounded(supports[0]["price"]) if supports else (major_lows[0] if major_lows else 0.0),
        "support_touches": int(supports[0]["touches"]) if supports else (1 if major_lows else 0),
        "resistance": rounded(resistances[0]["price"]) if resistances else (major_highs[0] if major_highs else 0.0),
        "resistance_touches": int(resistances[0]["touches"]) if resistances else (1 if major_highs else 0),
        "major_supports": major_lows,
        "major_resistances": major_highs,
        "basis": "close" if close_only else "ohlc",
    }


def divergence(frame: pd.DataFrame, indicator: pd.Series, lookback: int = 80) -> list[str]:
    start = max(0, len(frame) - lookback)
    lows = [(index, value) for index, value in confirmed_pivots(frame["Low"], mode="low") if index >= start]
    highs = [(index, value) for index, value in confirmed_pivots(frame["High"], mode="high") if index >= start]
    findings: list[str] = []
    if len(lows) >= 2:
        (first_i, first_p), (last_i, last_p) = lows[-2], lows[-1]
        first_v, last_v = float(indicator.iloc[first_i]), float(indicator.iloc[last_i])
        if len(frame) - last_i <= max(10, lookback // 4) and math.isfinite(first_v) and math.isfinite(last_v):
            if last_p < first_p and last_v > first_v:
                findings.append("pozitif")
            elif last_p > first_p and last_v < first_v:
                findings.append("gizli_pozitif")
    if len(highs) >= 2:
        (first_i, first_p), (last_i, last_p) = highs[-2], highs[-1]
        first_v, last_v = float(indicator.iloc[first_i]), float(indicator.iloc[last_i])
        if len(frame) - last_i <= max(10, lookback // 4) and math.isfinite(first_v) and math.isfinite(last_v):
            if last_p > first_p and last_v < first_v:
                findings.append("negatif")
            elif last_p < first_p and last_v > first_v:
                findings.append("gizli_negatif")
    return findings


def candle_pattern(frame: pd.DataFrame) -> str:
    if len(frame) < 2:
        return "yok"
    previous = frame.iloc[-2]
    current = frame.iloc[-1]
    body = abs(float(current["Close"] - current["Open"]))
    candle_range = max(float(current["High"] - current["Low"]), 1e-9)
    upper = float(current["High"] - max(current["Open"], current["Close"]))
    lower = float(min(current["Open"], current["Close"]) - current["Low"])
    bullish = current["Close"] > current["Open"]
    bearish = current["Close"] < current["Open"]
    if bullish and current["Open"] <= previous["Close"] and current["Close"] >= previous["Open"]:
        return "bullish_engulfing"
    if bearish and current["Open"] >= previous["Close"] and current["Close"] <= previous["Open"]:
        return "bearish_engulfing"
    if body / candle_range <= 0.55 and lower >= max(body * 2, candle_range * 0.35):
        return "hammer"
    if body / candle_range <= 0.55 and upper >= max(body * 2, candle_range * 0.35):
        return "shooting_star"
    if body / candle_range >= 0.70:
        return "strong_bull" if bullish else "strong_bear"
    return "yok"


def score_status(score: int, bullish_trigger: bool, risk: bool = False) -> str:
    if risk:
        return "risk"
    if score >= 7 and bullish_trigger:
        return "AL"
    if score >= 5 and not bullish_trigger:
        return "tepki_bekleniyor"
    if score >= 5:
        return "hazirlik"
    return "zayif"


def _finite_last(series: pd.Series) -> float:
    value = float(series.iloc[-1])
    return value if math.isfinite(value) else 0.0


def analyze_intraday_tl(frame_5m: pd.DataFrame, frame_15m: pd.DataFrame, now: datetime | None = None) -> ScoreResult | None:
    data_5m = completed_bars(frame_5m, "5m", now)
    data_15m = completed_bars(frame_15m, "15m", now)
    if data_5m is None or data_15m is None or len(data_5m) < 210 or len(data_15m) < 210:
        return None

    close_5m, close_15m = data_5m["Close"], data_15m["Close"]
    price = float(close_5m.iloc[-1])
    e9, e21, e50, e200 = (ema(close_5m, period) for period in (9, 21, 50, 200))
    rsi_5m, rsi_15m = rsi(close_5m), rsi(close_15m)
    macd_5m, macd_signal_5m, _ = macd(close_5m)
    macd_15m, macd_signal_15m, _ = macd(close_15m)
    vwap_5m, vwap_15m = intraday_vwap(data_5m), intraday_vwap(data_15m)
    cmf_5m, cmf_15m = cmf(data_5m), cmf(data_15m)
    rvol_5m, rvol_15m = relative_volume(data_5m), relative_volume(data_15m)
    line_15m, trend_15m, flips_15m = dual_range_state(close_15m)
    levels = level_map(data_15m)
    atr_5m = _finite_last(atr(data_5m))
    support = float(levels["support"])
    near_support = support > 0 and abs(price - support) <= max(atr_5m, price * 0.006)
    last_15m = data_15m.iloc[-1]
    support_reaction = (
        support > 0
        and float(last_15m["Low"]) <= support + max(atr_5m, price * 0.003)
        and float(last_15m["Close"]) > float(last_15m["Open"])
        and float(last_15m["Close"]) > support
    )
    bullish_flip = bool(flips_15m.iloc[-1] and trend_15m.iloc[-1] == 1)

    score = 0
    details: list[str] = []
    if price > _finite_last(e9) > _finite_last(e21):
        score += 1
        details.append("5dk EMA9/21 pozitif")
    if price > _finite_last(e50):
        score += 1
        details.append("5dk EMA50 ustu")
    if price > _finite_last(e200):
        score += 1
        details.append("5dk EMA200 ustu")
    if price > _finite_last(vwap_5m) and float(close_15m.iloc[-1]) > _finite_last(vwap_15m):
        score += 1
        details.append("5dk ve 15dk VWAP ustu")
    rsi_value_5m, rsi_value_15m = _finite_last(rsi_5m), _finite_last(rsi_15m)
    if 45 <= rsi_value_5m <= 72 and 45 <= rsi_value_15m <= 72:
        score += 1
        details.append("RSI dengeli guclu")
    if _finite_last(macd_5m) > _finite_last(macd_signal_5m) and _finite_last(macd_15m) > _finite_last(macd_signal_15m):
        score += 1
        details.append("MACD 5dk/15dk pozitif")
    if _finite_last(cmf_5m) > 0 and _finite_last(cmf_15m) > 0:
        score += 1
        details.append("CMF alici baskisi")
    if max(_finite_last(rvol_5m), _finite_last(rvol_15m)) >= 1.3:
        score += 1
        details.append("goreli hacim artisi")
    rsi_div = divergence(data_15m, rsi_15m)
    macd_div = divergence(data_15m, macd_15m)
    if any(label in ("pozitif", "gizli_pozitif") for label in rsi_div + macd_div):
        score += 1
        details.append("15dk pozitif uyumsuzluk")
    negative_divergence = any(label in ("negatif", "gizli_negatif") for label in rsi_div + macd_div)
    if negative_divergence:
        score -= 1
        details.append("15dk negatif uyumsuzluk riski")
    if bullish_flip or support_reaction or (trend_15m.iloc[-1] == 1 and near_support):
        score += 1
        details.append("15dk trend/tepki teyidi")

    risk = rsi_value_5m >= 80 or rsi_value_15m >= 80 or negative_divergence
    score = max(0, min(10, score))
    status = score_status(score, bullish_flip or support_reaction or (near_support and trend_15m.iloc[-1] == 1), risk)
    return ScoreResult(
        score=score,
        status=status,
        price=price,
        details=details,
        metrics={
            "rsi_5m": rsi_value_5m,
            "rsi_15m": rsi_value_15m,
            "volume_x_5m": _finite_last(rvol_5m),
            "volume_x_15m": _finite_last(rvol_15m),
            "cmf_5m": _finite_last(cmf_5m),
            "cmf_15m": _finite_last(cmf_15m),
            "vwap_5m": _finite_last(vwap_5m),
            "vwap_15m": _finite_last(vwap_15m),
            "range_line_15m": _finite_last(line_15m),
            "range_trend_15m": int(trend_15m.iloc[-1]),
            "support": levels["support"],
            "resistance": levels["resistance"],
            "support_reaction": support_reaction,
            "rsi_divergence": rsi_div,
            "macd_divergence": macd_div,
            "stop_loss": max(0.0, price - (1.5 * atr_5m)),
            "bar_closed_at_5m": data_5m.index[-1],
            "bar_closed_at_15m": data_15m.index[-1],
        },
    )


def analyze_daily(frame: pd.DataFrame, currency: str, close_only: bool = False, now: datetime | None = None) -> ScoreResult | None:
    data = completed_bars(frame, "1d", now)
    if data is None or len(data) < 210:
        return None
    close = data["Close"]
    price = float(close.iloc[-1])
    s20, s50, s200 = (sma(close, period) for period in (20, 50, 200))
    rsi_series = rsi(close)
    macd_line, macd_signal, _ = macd(close)
    line, trend, flips = dual_range_state(close)
    levels = level_map(data, close_only=close_only)
    price_atr = _finite_last(atr(data)) if not close_only else _finite_last(close.diff().abs().rolling(14).mean())
    support = float(levels["support"])
    resistance = float(levels["resistance"])
    near_support = support > 0 and abs(price - support) <= max(price_atr, price * 0.012)
    last_bar = data.iloc[-1]
    support_reaction = (
        not close_only
        and support > 0
        and float(last_bar["Low"]) <= support + max(price_atr, price * 0.006)
        and float(last_bar["Close"]) > float(last_bar["Open"])
        and float(last_bar["Close"]) > support
    )
    room_pct = ((resistance - price) / price) if resistance > price else 0.0
    bullish_flip = bool(flips.iloc[-1] and trend.iloc[-1] == 1)
    rsi_value = _finite_last(rsi_series)
    rsi_div = divergence(data.assign(Low=close, High=close) if close_only else data, rsi_series)
    macd_div = divergence(data.assign(Low=close, High=close) if close_only else data, macd_line)

    score = 0
    details: list[str] = []
    if price > _finite_last(s20) and _finite_last(s20) > _finite_last(s50):
        score += 1
        details.append("SMA20/50 pozitif")
    if price > _finite_last(s200):
        score += 1
        details.append("SMA200 ustu")
    if trend.iloc[-1] == 1:
        score += 1
        details.append("range trend pozitif")
    if 45 <= rsi_value <= 70:
        score += 1
        details.append("RSI dengeli guclu")
    if _finite_last(macd_line) > _finite_last(macd_signal):
        score += 1
        details.append("MACD pozitif")
    if any(label in ("pozitif", "gizli_pozitif") for label in rsi_div + macd_div):
        score += 1
        details.append("pozitif uyumsuzluk")
    negative_divergence = any(label in ("negatif", "gizli_negatif") for label in rsi_div + macd_div)
    if negative_divergence:
        score -= 1
        details.append("negatif uyumsuzluk riski")
    if near_support or room_pct >= 0.04 or resistance == 0:
        score += 1
        details.append("destek bolgesinde" if near_support else "ust dirence alan var")
    if bullish_flip:
        score += 1
        details.append("yeni trend donusu")
    elif support_reaction:
        score += 1
        details.append("destekten tepki mumu")
    metrics: dict[str, Any] = {
        "currency": currency,
        "sma20": _finite_last(s20),
        "sma50": _finite_last(s50),
        "sma200": _finite_last(s200),
        "rsi": rsi_value,
        "macd": _finite_last(macd_line),
        "macd_signal": _finite_last(macd_signal),
        "range_line": _finite_last(line),
        "range_trend": int(trend.iloc[-1]),
        "support": levels["support"],
        "support_touches": levels["support_touches"],
        "resistance": levels["resistance"],
        "resistance_touches": levels["resistance_touches"],
        "support_reaction": support_reaction,
        "room_pct": room_pct * 100,
        "rsi_divergence": rsi_div,
        "macd_divergence": macd_div,
        "price_basis": "close_only" if close_only else "ohlc",
        "bar_closed_at": data.index[-1],
    }

    if currency == "TL":
        flow = cmf(data)
        rvol = relative_volume(data)
        obv_series = obv(data)
        obv_baseline = sma(obv_series, 20)
        cmf_positive = _finite_last(flow) > 0
        obv_accumulation = _finite_last(obv_series) > _finite_last(obv_baseline)
        flow_confirmed = cmf_positive or obv_accumulation
        if flow_confirmed:
            score += 1
            if cmf_positive and obv_accumulation:
                details.append("CMF ve OBV para girisini destekliyor")
            elif cmf_positive:
                details.append("CMF pozitif para akisi")
            else:
                details.append("OBV birikim egiliminde")
        rvol_value = _finite_last(rvol)
        if rvol_value >= 1.3:
            score += 1
            details.append("goreli hacim artisi")
        metrics.update({
            "cmf": _finite_last(flow), "volume_x": rvol_value,
            "obv_above_sma20": obv_accumulation,
            "obv_slope_20": _finite_last(obv_series.diff(20)),
        })
        metrics["pattern"] = candle_pattern(data)

    risk = negative_divergence or rsi_value >= 80 or (resistance > 0 and room_pct < 0.01 and price < resistance)
    score = max(0, min(10, score))
    status = score_status(score, bullish_flip or support_reaction or (near_support and trend.iloc[-1] == 1), risk)
    return ScoreResult(score=score, status=status, price=price, details=details, metrics=metrics)


def build_usd_close_frame(tl_frame: pd.DataFrame, usdtry_frame: pd.DataFrame) -> pd.DataFrame | None:
    tl = clean_ohlcv(tl_frame)
    fx = clean_ohlcv(usdtry_frame)
    if tl is None or fx is None:
        return None
    tl_close = tl["Close"].copy()
    fx_close = fx["Close"].copy()
    tl_close.index = pd.DatetimeIndex(tl_close.index).normalize()
    fx_close.index = pd.DatetimeIndex(fx_close.index).normalize()
    joined = pd.concat([tl_close.rename("tl"), fx_close.rename("fx")], axis=1, join="inner").dropna()
    joined = joined[joined["fx"] > 0]
    if joined.empty:
        return None
    usd_close = joined["tl"] / joined["fx"]
    volume = tl["Volume"].copy()
    volume.index = pd.DatetimeIndex(volume.index).normalize()
    output = pd.DataFrame(index=usd_close.index)
    output["Close"] = usd_close
    output["Open"] = usd_close
    output["High"] = usd_close
    output["Low"] = usd_close
    output["Volume"] = volume.reindex(output.index).fillna(0)
    return output


def analyze_daily_pair(
    tl_frame: pd.DataFrame,
    usdtry_frame: pd.DataFrame | None,
    now: datetime | None = None,
) -> tuple[ScoreResult | None, ScoreResult | None, dict[str, Any]]:
    tl_result = analyze_daily(tl_frame, "TL", close_only=False, now=now)
    tl_completed = completed_bars(tl_frame, "1d", now)
    fx_completed = completed_bars(usdtry_frame, "1d", now) if usdtry_frame is not None else None
    same_session_fx = False
    if tl_completed is not None and fx_completed is not None:
        tl_day = pd.Timestamp(tl_completed.index[-1]).date()
        fx_day = pd.Timestamp(fx_completed.index[-1]).date()
        same_session_fx = tl_day == fx_day
    # Never silently attach an older FX close to a newer stock session.
    usd_frame = build_usd_close_frame(tl_frame, fx_completed) if same_session_fx else None
    usd_result = analyze_daily(usd_frame, "USD", close_only=True, now=now) if usd_frame is not None else None
    fx = fx_completed if same_session_fx else None
    metadata = {
        "usd_data_quality": "close_only" if usd_result is not None else "missing",
        "usdtry": rounded(fx["Close"].iloc[-1]) if fx is not None else 0.0,
        "usdtry_time": fx.index[-1].isoformat() if fx is not None else "",
    }
    return tl_result, usd_result, metadata
