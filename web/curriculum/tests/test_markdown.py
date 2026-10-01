from curriculum.markdown import render


def test_script_tag_is_not_html():
    out = render("hello\n\n<script>alert(1)</script>\n")
    assert "<script" not in out
    assert "&lt;script&gt;" in out  # shown as text, never run


def test_javascript_link_dropped():
    out = render("[x](javascript:alert(1)) [y](https://example.com)")
    assert 'href="javascript' not in out  # left as text, never a link
    assert 'href="https://example.com"' in out
    assert 'rel="noopener noreferrer"' in out


def test_lesson_markdown_features():
    out = render("## Head\n\n`print x`\n\n```\n(gdb) run\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")
    for tag in ("<h2>", "<code>print x</code>", "<pre><code>", "<table>", "<td>1</td>"):
        assert tag in out


def test_event_handler_attributes_stripped():
    out = render('<img src=x onerror="alert(1)">\n\n<a href="#" onclick="x()">a</a>')
    assert "<img" not in out and "<a " not in out  # only escaped text remains
