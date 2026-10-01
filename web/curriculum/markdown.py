"""Lesson and solution markdown to safe HTML (spec "Web app": a safe renderer).

Two layers: markdown-it with raw HTML off escapes any HTML in the source, and nh3 then keeps
only the tags lessons use. Code blocks get CSS only, no highlighter (plan 6, "Markdown").
"""

import nh3
from django.utils.safestring import SafeString, mark_safe
from markdown_it import MarkdownIt

_md = MarkdownIt("commonmark", {"html": False, "linkify": False, "typographer": False}).enable(
    "table"
)

TAGS = {
    "a", "blockquote", "br", "code", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "kbd",
    "li", "ol", "p", "pre", "strong", "table", "tbody", "td", "th", "thead", "tr", "ul",
}  # fmt: skip
ATTRIBUTES = {"a": {"href", "title"}, "code": {"class"}, "th": {"style"}, "td": {"style"}}


def render(md: str) -> SafeString:
    html = _md.render(md)
    clean = nh3.clean(
        html,
        tags=TAGS,
        attributes=ATTRIBUTES,
        url_schemes={"http", "https", "mailto"},
        link_rel="noopener noreferrer",
    )
    return mark_safe(clean)  # noqa: S308  sanitised by nh3 just above
