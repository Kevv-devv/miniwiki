#!/usr/bin/env bash
# Capture REAL MiniWiki screenshots from a disposable, public demo instance.
# Usage: bash scripts/capture_screenshots.sh [http://127.0.0.1:9090]
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base="${1:-http://127.0.0.1:9090}"
base="${base%/}"

browser=""
engine="chromium"
for candidate in chromium chromium-browser google-chrome google-chrome-stable; do
  if command -v "$candidate" >/dev/null 2>&1; then
    browser="$(command -v "$candidate")"
    break
  fi
done
if [[ -z "$browser" ]] && command -v firefox >/dev/null 2>&1; then
  browser="$(command -v firefox)"
  engine="firefox"
fi
if [[ -z "$browser" ]]; then
  echo "Install Chromium or Firefox, or take screenshots manually." >&2
  exit 1
fi

if ! curl -fsS -o /dev/null "$base/wiki/main-page"; then
  echo "Can't reach MiniWiki at $base. Start your throwaway demo instance first." >&2
  exit 1
fi

mkdir -p "$root/docs/images"
tmp_profile="$(mktemp -d)"
trap 'rm -rf "$tmp_profile"' EXIT

pages=(
  'wiki/main-page:wiki-home.png'
  'pages:all-pages.png'
  'search:search.png'
)
for item in "${pages[@]}"; do
  route="${item%%:*}"
  filename="${item##*:}"
  actual="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$base/$route")"
  if [[ "$actual" == *'/login'* ]]; then
    echo "Refusing to screenshot login page for $route; use a demo with public content." >&2
    exit 1
  fi
  if [[ "$engine" == "chromium" ]]; then
    "$browser" --headless --disable-gpu --hide-scrollbars \
      --no-first-run --no-default-browser-check \
      --user-data-dir="$tmp_profile" \
      --window-size=1366,900 --force-device-scale-factor=1 \
      --virtual-time-budget=1800 \
      --screenshot="$root/docs/images/$filename" \
      "$base/$route" >/dev/null 2>&1
  else
    "$browser" --headless --no-remote --profile "$tmp_profile" \
      --window-size 1366,900 \
      --screenshot "$root/docs/images/$filename" \
      "$base/$route" >/dev/null 2>&1
  fi
  test -s "$root/docs/images/$filename"
  echo "Saved: docs/images/$filename"
done

python3 - "$root/README.md" <<'PY'
from pathlib import Path
import sys
readme = Path(sys.argv[1])
s = readme.read_text()
start = '<!-- SCREENSHOT-GALLERY-START -->'
end = '<!-- SCREENSHOT-GALLERY-END -->'
if s.count(start) != 1 or s.count(end) != 1:
    raise SystemExit('Unexpected README screenshot markers; images saved but README not changed')
gallery = '''<!-- SCREENSHOT-GALLERY-START -->
![MiniWiki wiki home page](docs/images/wiki-home.png)

<details><summary>All pages and search</summary>

![MiniWiki all pages](docs/images/all-pages.png)

![MiniWiki search](docs/images/search.png)

</details>
<!-- SCREENSHOT-GALLERY-END -->'''
begin = s.index(start)
finish = s.index(end) + len(end)
readme.write_text(s[:begin] + gallery + s[finish:])
print('README screenshot gallery enabled. Review images for private content before committing.')
PY
