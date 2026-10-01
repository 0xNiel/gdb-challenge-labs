"""Small server-side SVG charts for the dashboards (plan 7: no Chart.js unless SVG gets painful).

Two shapes: a line chart over time, one or more series; and a horizontal bar chart. Output is
a complete <svg> element, safe to put in a template: every label is escaped. Colours come from
CSS classes (site.css), so the charts follow the site's palette.
"""

from datetime import datetime
from html import escape

from django.utils.safestring import SafeString, mark_safe

W, H = 640, 220  # viewBox; the CSS scales it to the container
PAD_L, PAD_R, PAD_T, PAD_B = 48, 12, 12, 28


def _nice_max(v: float) -> float:
    if v <= 0:
        return 1.0
    mag = 10 ** (len(str(int(v))) - 1)
    for step in (1, 2, 2.5, 5, 10):
        if v <= step * mag:
            return step * mag
    return 10 * mag


def _fmt(v: float) -> str:
    return f"{v:.0f}" if abs(v) >= 10 or v == int(v) else f"{v:.1f}"


def line_chart(
    series: list[tuple[str, list[tuple[datetime, float]]]], unit: str = ""
) -> SafeString:
    """Lines over time. series: [(name, [(time, value), ...]), ...], each sorted by time."""
    pts = [p for _, s in series for p in s]
    out = [
        f'<svg class="chart" viewBox="0 0 {W} {H}" role="img" xmlns="http://www.w3.org/2000/svg">'
    ]
    if not pts:
        out.append(
            f'<text class="empty" x="{W / 2}" y="{H / 2}" text-anchor="middle">No data yet</text></svg>'
        )
        return mark_safe("".join(out))  # noqa: S308  built from escaped parts
    t0 = min(t for t, _ in pts)
    t1 = max(t for t, _ in pts)
    span = max((t1 - t0).total_seconds(), 1.0)
    ymax = _nice_max(max(v for _, v in pts))
    pw, ph = W - PAD_L - PAD_R, H - PAD_T - PAD_B

    def x(t):
        return PAD_L + pw * (t - t0).total_seconds() / span

    def y(v):
        return PAD_T + ph * (1 - v / ymax)

    for i in range(5):  # gridlines and y labels
        v = ymax * i / 4
        out.append(
            f'<line class="grid" x1="{PAD_L}" x2="{W - PAD_R}" y1="{y(v):.1f}" y2="{y(v):.1f}"/>'
        )
        out.append(
            f'<text class="axis" x="{PAD_L - 6}" y="{y(v) + 4:.1f}" text-anchor="end">{_fmt(v)}</text>'
        )
    fmt = "%H:%M" if span <= 86400 * 2 else "%b %d"
    for t, anchor in ((t0, "start"), (t1, "end")):
        out.append(
            f'<text class="axis" x="{x(t):.1f}" y="{H - 8}" text-anchor="{anchor}">{escape(t.strftime(fmt))}</text>'
        )
    if unit:
        out.append(f'<text class="axis" x="4" y="{PAD_T + 4}">{escape(unit)}</text>')
    for i, (name, s) in enumerate(series):
        if not s:
            continue
        d = " ".join(f"{x(t):.1f},{y(v):.1f}" for t, v in s)
        out.append(
            f'<polyline class="series s{i % 4}" points="{d}"><title>{escape(name)}</title></polyline>'
        )
    legend = "".join(
        f'<text class="legend s{i % 4}" x="{PAD_L + 8 + 150 * i}" y="{PAD_T + 12}">{escape(name)}</text>'
        for i, (name, _) in enumerate(series)
        if len(series) > 1
    )
    out.append(legend + "</svg>")
    return mark_safe("".join(out))  # noqa: S308  built from escaped parts


def bar_chart(rows: list[tuple[str, float]], unit: str = "") -> SafeString:
    """Horizontal bars: [(label, value), ...] in the order given."""
    bh, gap, label_w = 20, 6, 220
    h = max(len(rows), 1) * (bh + gap) + 8
    out = [
        f'<svg class="chart bars" viewBox="0 0 {W} {h}" role="img" xmlns="http://www.w3.org/2000/svg">'
    ]
    if not rows:
        out.append(
            f'<text class="empty" x="{W / 2}" y="{h / 2 + 4}" text-anchor="middle">No data yet</text></svg>'
        )
        return mark_safe("".join(out))  # noqa: S308  built from escaped parts
    vmax = max((v for _, v in rows), default=0) or 1
    bw = W - label_w - 70
    for i, (label, v) in enumerate(rows):
        top = 4 + i * (bh + gap)
        w = max(bw * v / vmax, 1)
        out.append(
            f'<text class="axis" x="{label_w - 8}" y="{top + 14}" text-anchor="end">{escape(label[:34])}</text>'
        )
        out.append(f'<rect class="bar" x="{label_w}" y="{top}" width="{w:.1f}" height="{bh}"/>')
        out.append(
            f'<text class="axis" x="{label_w + w + 6:.1f}" y="{top + 14}">{_fmt(v)}{escape(unit)}</text>'
        )
    out.append("</svg>")
    return mark_safe("".join(out))  # noqa: S308  built from escaped parts
