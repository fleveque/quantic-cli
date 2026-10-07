"""Builds a walkthrough page from a template, pulling every code excerpt
verbatim from the repository so the page can't drift from the source.

Usage: python3 build.py <dir> <output-name> <template-file>
  reads <dir>/<template-file>, writes <dir>/<output-name>.html

Marker: [[path|start|end]] — the excerpt runs from the first line containing
`start` to the first line at or after it containing `end`. Paths are
relative to the repository root. Build from a clean checkout of the commit
the page names, so the excerpts are that commit's.
"""
import html
import os
import re
import sys

REPO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "..") + "/"
HERE = sys.argv[1]


def excerpt(m):
    path, start, end = m.group(1), m.group(2), m.group(3)
    lines = open(REPO + path).read().split("\n")
    # end forms: "}" = next line that is exactly "}" (end of a top-level
    # declaration); "A>B" = next line containing A, then the next line whose
    # stripped text is B; anything else = next line containing it.
    try:
        a = next(i for i, l in enumerate(lines) if start in l)
        if end == "}":
            b = next(i for i in range(a, len(lines)) if lines[i] == "}")
        elif ">" in end:
            first, closer = end.split(">", 1)
            f = next(i for i in range(a, len(lines)) if first in lines[i])
            b = next(i for i in range(f + 1, len(lines)) if lines[i].strip() == closer)
        else:
            b = next(i for i in range(a, len(lines)) if end in lines[i])
    except StopIteration:
        sys.exit(f"marker not found: {m.group(0)}")
    code = "\n".join(lines[a : b + 1])
    lang = {"go": "go", "sql": "sql"}.get(path.rsplit(".", 1)[-1], "plaintext")
    return (
        f'<figure class="code"><figcaption><span class="file">{path}</span>'
        f'<span class="lines">lines {a + 1}–{b + 1}</span></figcaption>'
        f'<pre><code class="language-{lang}">{html.escape(code, quote=False)}</code></pre></figure>'
    )


NAME = sys.argv[2] if len(sys.argv) > 2 else "walkthrough"
src = open(HERE + "/" + (sys.argv[3] if len(sys.argv) > 3 else "template.html")).read()
out, n = re.subn(r"\[\[([^|\]]+)\|([^|\]]+)\|([^\]]+)\]\]", excerpt, src)
open(HERE + "/" + NAME + ".html", "w").write(out)
print(f"{n} excerpts, {len(out) // 1024} KB")
