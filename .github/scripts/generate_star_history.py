#!/usr/bin/env python3
"""Generate self-hosted light and dark Star History SVG charts.

The script only uses the Python standard library. In GitHub Actions it reads the
short-lived GITHUB_TOKEN, fetches the repository's current stargazers and their
timestamps, and writes two static SVG files.
"""

from __future__ import annotations

import argparse
import html
import json
import math
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable, Sequence


WIDTH = 900
HEIGHT = 560
PLOT_LEFT = 82
PLOT_RIGHT = 852
PLOT_TOP = 116
PLOT_BOTTOM = 480
API_VERSION = "2026-03-10"
REPOSITORY_PATTERN = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")


THEMES = {
    "light": {
        "background": "#ffffff",
        "panel": "#ffffff",
        "border": "#d0d7de",
        "grid": "#d8dee4",
        "text": "#1f2328",
        "muted": "#656d76",
        "line": "#1f883d",
        "fill": "#2da44e",
        "badge": "#f6f8fa",
    },
    "dark": {
        "background": "#0d1117",
        "panel": "#0d1117",
        "border": "#30363d",
        "grid": "#30363d",
        "text": "#e6edf3",
        "muted": "#8b949e",
        "line": "#3fb950",
        "fill": "#2ea043",
        "badge": "#161b22",
    },
}


def parse_timestamp(value: str) -> datetime:
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def fetch_stargazer_timestamps(repository: str, token: str) -> list[datetime]:
    if not REPOSITORY_PATTERN.fullmatch(repository):
        raise ValueError("repository must use the OWNER/REPO format")
    if not token:
        raise ValueError("GITHUB_TOKEN is required to read stargazer timestamps")

    owner, repo = repository.split("/", 1)
    encoded_owner = urllib.parse.quote(owner, safe="")
    encoded_repo = urllib.parse.quote(repo, safe="")
    page = 1
    timestamps: list[datetime] = []

    while True:
        url = (
            f"https://api.github.com/repos/{encoded_owner}/{encoded_repo}/stargazers"
            f"?per_page=100&page={page}"
        )
        request = urllib.request.Request(
            url,
            headers={
                "Accept": "application/vnd.github.star+json",
                "Authorization": f"Bearer {token}",
                "User-Agent": "clawmanager-star-history-workflow",
                "X-GitHub-Api-Version": API_VERSION,
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                batch = json.load(response)
        except urllib.error.HTTPError as error:
            detail = error.read().decode("utf-8", errors="replace")
            raise RuntimeError(
                f"GitHub stargazers API returned HTTP {error.code}: {detail}"
            ) from error

        if not isinstance(batch, list):
            raise RuntimeError("GitHub stargazers API returned an unexpected response")

        for item in batch:
            starred_at = item.get("starred_at") if isinstance(item, dict) else None
            if not starred_at:
                raise RuntimeError(
                    "GitHub did not return star timestamps; verify the API token and Accept header"
                )
            timestamps.append(parse_timestamp(starred_at))

        print(f"Fetched page {page}: {len(batch)} stargazers", file=sys.stderr)
        if len(batch) < 100:
            break
        page += 1

    return sorted(timestamps)


def nice_ceiling(value: int) -> int:
    if value <= 5:
        return max(1, value)
    magnitude = 10 ** math.floor(math.log10(value))
    normalized = value / magnitude
    for step in (1, 2, 2.5, 5, 10):
        if normalized <= step:
            return int(step * magnitude)
    return int(10 * magnitude)


def compact_number(value: int) -> str:
    if value >= 1_000_000:
        return f"{value / 1_000_000:.1f}M".replace(".0M", "M")
    if value >= 1_000:
        return f"{value / 1_000:.1f}k".replace(".0k", "k")
    return str(value)


def x_label(moment: datetime, span_days: float) -> str:
    if span_days >= 365 * 4:
        return moment.strftime("%Y")
    if span_days >= 365:
        return moment.strftime("%b %Y")
    if span_days >= 90:
        return moment.strftime("%b %Y")
    return moment.strftime("%d %b")


def chart_points(
    timestamps: Sequence[datetime], updated_at: datetime, y_max: int
) -> tuple[list[tuple[float, float]], datetime, datetime]:
    if timestamps:
        start = timestamps[0]
        end = max(updated_at, timestamps[-1])
    else:
        end = updated_at
        start = updated_at - timedelta(days=365)

    span = max((end - start).total_seconds(), 1.0)
    plot_width = PLOT_RIGHT - PLOT_LEFT
    plot_height = PLOT_BOTTOM - PLOT_TOP
    points: list[tuple[float, float]] = [(PLOT_LEFT, PLOT_BOTTOM)]
    last_pixel = -1

    for index, timestamp in enumerate(timestamps, start=1):
        x = PLOT_LEFT + ((timestamp - start).total_seconds() / span) * plot_width
        y = PLOT_BOTTOM - (index / y_max) * plot_height
        pixel = int(x)
        if pixel == last_pixel and len(points) > 1:
            points[-1] = (x, y)
        else:
            points.append((x, y))
            last_pixel = pixel

    if timestamps:
        points.append((PLOT_RIGHT, points[-1][1]))
    return points, start, end


def path_data(points: Iterable[tuple[float, float]]) -> str:
    iterator = iter(points)
    first = next(iterator)
    commands = [f"M {first[0]:.2f} {first[1]:.2f}"]
    commands.extend(f"L {x:.2f} {y:.2f}" for x, y in iterator)
    return " ".join(commands)


def render_svg(
    repository: str,
    timestamps: Sequence[datetime],
    updated_at: datetime,
    theme_name: str,
) -> str:
    theme = THEMES[theme_name]
    star_count = len(timestamps)
    y_max = nice_ceiling(star_count)
    points, start, end = chart_points(timestamps, updated_at, y_max)
    line_path = path_data(points)
    fill_path = (
        f"{line_path} L {PLOT_RIGHT:.2f} {PLOT_BOTTOM:.2f} "
        f"L {PLOT_LEFT:.2f} {PLOT_BOTTOM:.2f} Z"
    )
    span_days = max((end - start).total_seconds() / 86400, 1)
    safe_repository = html.escape(repository)
    safe_date = html.escape(updated_at.strftime("%Y-%m-%d UTC"))

    y_grid: list[str] = []
    for index in range(6):
        value = round(y_max * index / 5)
        y = PLOT_BOTTOM - (PLOT_BOTTOM - PLOT_TOP) * index / 5
        y_grid.append(
            f'<path d="M {PLOT_LEFT} {y:.2f} L {PLOT_RIGHT} {y:.2f}" '
            f'stroke="{theme["grid"]}" stroke-width="1" stroke-dasharray="4 7" />'
        )
        y_grid.append(
            f'<text x="{PLOT_LEFT - 14}" y="{y + 5:.2f}" text-anchor="end" '
            f'class="tick">{compact_number(value)}</text>'
        )

    x_grid: list[str] = []
    for index in range(6):
        fraction = index / 5
        x = PLOT_LEFT + (PLOT_RIGHT - PLOT_LEFT) * fraction
        moment = start + (end - start) * fraction
        anchor = "start" if index == 0 else "end" if index == 5 else "middle"
        x_grid.append(
            f'<path d="M {x:.2f} {PLOT_TOP} L {x:.2f} {PLOT_BOTTOM}" '
            f'stroke="{theme["grid"]}" stroke-width="1" stroke-dasharray="4 7" />'
        )
        x_grid.append(
            f'<text x="{x:.2f}" y="{PLOT_BOTTOM + 30}" text-anchor="{anchor}" '
            f'class="tick">{html.escape(x_label(moment, span_days))}</text>'
        )

    empty_state = ""
    if not timestamps:
        empty_state = (
            f'<text x="{(PLOT_LEFT + PLOT_RIGHT) / 2:.2f}" '
            f'y="{(PLOT_TOP + PLOT_BOTTOM) / 2:.2f}" text-anchor="middle" '
            f'class="empty">No stars yet — be the first!</text>'
        )

    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{WIDTH}" height="{HEIGHT}" viewBox="0 0 {WIDTH} {HEIGHT}" role="img" aria-labelledby="title description">
  <title id="title">Star History for {safe_repository}</title>
  <desc id="description">{star_count} current GitHub stars, chart updated {safe_date}</desc>
  <defs>
    <linearGradient id="area" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="{theme['fill']}" stop-opacity="0.28" />
      <stop offset="100%" stop-color="{theme['fill']}" stop-opacity="0.02" />
    </linearGradient>
    <filter id="sketch" x="-3%" y="-3%" width="106%" height="106%">
      <feTurbulence type="fractalNoise" baseFrequency="0.012 0.035" numOctaves="1" seed="23" result="noise" />
      <feDisplacementMap in="SourceGraphic" in2="noise" scale="1.35" xChannelSelector="R" yChannelSelector="G" />
    </filter>
    <style>
      text {{ font-family: "Comic Sans MS", "Segoe Print", "Bradley Hand", ui-rounded, cursive; fill: {theme['text']}; }}
      .tick {{ font-size: 14px; fill: {theme['muted']}; }}
      .empty {{ font-size: 21px; fill: {theme['muted']}; }}
    </style>
  </defs>
  <rect width="{WIDTH}" height="{HEIGHT}" rx="16" fill="{theme['background']}" />
  <rect x="1" y="1" width="{WIDTH - 2}" height="{HEIGHT - 2}" rx="15" fill="none" stroke="{theme['border']}" />
  <text x="42" y="52" font-size="27" font-weight="700">Star History</text>
  <text x="858" y="52" text-anchor="end" font-size="25" font-weight="700">★ {compact_number(star_count)}</text>
  <g transform="translate(42 72)">
    <rect width="310" height="34" rx="17" fill="{theme['badge']}" stroke="{theme['border']}" />
    <path d="M 19 7 L 22 14 L 29 17 L 22 20 L 19 27 L 16 20 L 9 17 L 16 14 Z" fill="{theme['line']}" />
    <text x="38" y="23" font-size="15">{safe_repository}</text>
  </g>
  <g>
    {''.join(y_grid)}
    {''.join(x_grid)}
    <path d="M {PLOT_LEFT} {PLOT_TOP - 3} L {PLOT_LEFT} {PLOT_BOTTOM + 3} L {PLOT_RIGHT + 3} {PLOT_BOTTOM}" fill="none" stroke="{theme['muted']}" stroke-width="2" stroke-linecap="round" filter="url(#sketch)" />
    <path d="{fill_path}" fill="url(#area)" />
    <path d="{line_path}" fill="none" stroke="{theme['line']}" stroke-width="4" stroke-linecap="round" stroke-linejoin="round" filter="url(#sketch)" />
    {empty_state}
  </g>
  <text x="858" y="540" text-anchor="end" class="tick">Updated {safe_date} · generated inside this repository</text>
</svg>
'''


def write_charts(
    output_dir: Path,
    repository: str,
    timestamps: Sequence[datetime],
    updated_at: datetime,
) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    for theme_name in THEMES:
        destination = output_dir / f"star-history-{theme_name}.svg"
        destination.write_text(
            render_svg(repository, timestamps, updated_at, theme_name),
            encoding="utf-8",
            newline="\n",
        )
        print(f"Wrote {destination}", file=sys.stderr)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--repository",
        default=os.environ.get(
            "STAR_HISTORY_REPOSITORY", os.environ.get("GITHUB_REPOSITORY", "")
        ),
        help=(
            "GitHub repository in OWNER/REPO format "
            "(defaults to STAR_HISTORY_REPOSITORY, then GITHUB_REPOSITORY)"
        ),
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("generated-star-history"),
        help="directory for the generated SVG files",
    )
    parser.add_argument(
        "--updated-at",
        help="override the chart update time (ISO 8601; useful for deterministic tests)",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not REPOSITORY_PATTERN.fullmatch(args.repository):
        raise SystemExit("--repository or GITHUB_REPOSITORY must use OWNER/REPO format")
    updated_at = (
        parse_timestamp(args.updated_at)
        if args.updated_at
        else datetime.now(timezone.utc)
    )
    timestamps = fetch_stargazer_timestamps(
        args.repository, os.environ.get("GITHUB_TOKEN", "")
    )
    write_charts(args.output_dir, args.repository, timestamps, updated_at)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
