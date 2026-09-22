"""Presentation for the MCP OAuth login and consent pages.

Styling mirrors the Checkpoint app: the coral pendant mark, sharp corners,
warm neutral surfaces and dark-mode support, all inlined so the pages need no
external assets or CDNs.
"""

import html

from starlette.responses import HTMLResponse

_BRAND_MARK = (
    '<svg viewBox="0 0 100 150" role="img" aria-label="Checkpoint" xmlns="http://www.w3.org/2000/svg">'
    '<rect x="42" y="0" width="16" height="38" fill="currentColor"/>'
    '<rect x="22" y="34" width="56" height="112" rx="28" fill="none" stroke="currentColor" stroke-width="13"/>'
    '<circle cx="50" cy="92" r="12" fill="currentColor"/>'
    "</svg>"
)

_PAGE_CSS = """
:root {
  color-scheme: light dark;
  --background: #f7f6f3;
  --foreground: #1d1b1a;
  --surface: #ffffff;
  --input-bg: #faf9f7;
  --primary: #ff6b57;
  --primary-foreground: #1d1b1a;
  --primary-pressed: #e95a47;
  --primary-text: #c83d2d;
  --muted-foreground: #625e59;
  --border: #d8d3cd;
  --border-strong: #bdb7b0;
  --destructive: #c63e3e;
}
@media (prefers-color-scheme: dark) {
  :root {
    --background: #151514;
    --foreground: #f7f5f2;
    --surface: #211e1d;
    --input-bg: #1b1918;
    --primary-text: #ff8272;
    --muted-foreground: #b7b2ad;
    --border: #3d3937;
    --border-strong: #57514d;
    --destructive: #f06464;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--background);
  color: var(--foreground);
  font-family: Archivo, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  font-size: 15px;
  line-height: 1.55;
  -webkit-font-smoothing: antialiased;
}
.card {
  width: 100%;
  max-width: 380px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 28px 24px;
  background: var(--surface);
  border: 1px solid var(--border);
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  margin: 0;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--muted-foreground);
}
.brand svg { height: 22px; width: auto; color: var(--primary); }
h1 { margin: 0; font-size: 26px; font-weight: 800; line-height: 1.12; letter-spacing: -0.015em; }
.lede { margin: 0; font-size: 13.5px; color: var(--muted-foreground); }
.lede strong { color: var(--foreground); font-weight: 600; }
form { margin: 0; display: flex; flex-direction: column; gap: 12px; }
label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--muted-foreground);
}
input {
  height: 40px;
  width: 100%;
  padding: 0 10px;
  border: 1px solid var(--border);
  background: var(--input-bg);
  color: var(--foreground);
  font: inherit;
  font-size: 14px;
}
input:focus-visible { outline: none; border-color: var(--border-strong); }
button {
  height: 42px;
  padding: 0 14px;
  border: 0;
  background: var(--primary);
  color: var(--primary-foreground);
  font: inherit;
  font-size: 14px;
  font-weight: 800;
  cursor: pointer;
  transition: background-color 0.15s ease;
}
button:hover { background: var(--primary-pressed); }
button.secondary {
  background: transparent;
  border: 1px solid var(--border);
  color: var(--foreground);
  font-weight: 600;
}
button.secondary:hover { background: var(--input-bg); }
.actions { display: flex; flex-direction: column; gap: 10px; }
.actions.row { flex-direction: row; }
.actions.row button { flex: 1; }
.alert {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--destructive);
  color: var(--destructive);
  font-size: 13px;
}
.scope { margin: 0; font-size: 13.5px; color: var(--muted-foreground); }
.scope strong { color: var(--foreground); font-weight: 600; }
.fine { margin: 0; text-align: center; font-size: 12.5px; }
a { color: var(--primary-text); text-decoration: none; }
a:hover { text-decoration: underline; }
"""

LOGIN_BODY = """<h1>Sign in</h1>
<p class="lede">Enter your email and we'll send you a one-time code to sign in to Checkpoint.</p>
<form method="post" action="/login">
<input type="hidden" name="req" value="{req}">
<label for="email">Email
<input id="email" type="email" name="email" autocomplete="username" required>
</label>
{error}
<button type="submit">Send code</button>
</form>"""

CODE_BODY = """<h1>Check your email</h1>
<p class="lede">We sent a one-time code to <strong>{email}</strong>. Enter it below to continue.</p>
<form method="post" action="/login">
<input type="hidden" name="req" value="{req}">
<input type="hidden" name="flow" value="{flow}">
<input type="hidden" name="email" value="{email}">
<label for="code">One-time code
<input id="code" type="text" name="code" inputmode="numeric" autocomplete="one-time-code" required>
</label>
{error}
<button type="submit">Verify and sign in</button>
</form>
<div class="actions">
<form method="post" action="/login">
<input type="hidden" name="req" value="{req}">
<input type="hidden" name="email" value="{email}">
<button type="submit" class="secondary">Send a new code</button>
</form>
<a class="fine" href="/login?req={req}">Use a different email</a>
</div>"""

CONSENT_BODY = """<h1>Authorize {client}</h1>
<p class="lede">{client} is requesting access to your Checkpoint recordings.</p>
<p class="scope">Scope: <strong>{scopes}</strong></p>
<form method="post" action="/consent">
<input type="hidden" name="req" value="{req}">
<input type="hidden" name="csrf" value="{csrf}">
<div class="actions row">
<button type="submit" name="decision" value="allow">Allow</button>
<button type="submit" name="decision" value="deny" class="secondary">Deny</button>
</div>
</form>"""


def error_block(message: str | None) -> str:
    return f'<p class="alert" role="alert">{html.escape(message)}</p>' if message else ""


def page(title: str, body: str) -> str:
    return (
        '<!doctype html><html lang="en"><head><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width, initial-scale=1">'
        f"<title>{html.escape(title)} · Checkpoint</title>"
        f"<style>{_PAGE_CSS}</style></head>"
        f'<body><main class="card"><p class="brand">{_BRAND_MARK}Checkpoint</p>{body}</main></body></html>'
    )


def render(title: str, body: str, **values: str) -> HTMLResponse:
    return HTMLResponse(page(title, body.format(**values)))


def error_response(message: str) -> HTMLResponse:
    body = f'<h1>Something went wrong</h1><p class="alert" role="alert">{html.escape(message)}</p>'
    return HTMLResponse(page("Error", body), status_code=400)
