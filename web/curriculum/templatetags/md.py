from django import template

from curriculum.markdown import render

register = template.Library()


@register.filter(name="md")
def md(text: str):
    """Markdown to sanitised HTML (curriculum/markdown.py)."""
    return render(text or "")
