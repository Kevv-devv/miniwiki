# Capturing honest screenshots for MiniWiki's README

The repository does not include invented screenshots. Use a **separate demo wiki** with sample/public content, and capture the actual running interface. Do not publish screenshots of private pages, email addresses, API keys, session cookies, or real user accounts.

## Prepare a demo instance

If you are still running your fresh-release smoke test on Ubuntu at `http://127.0.0.1:9090`, you can reuse it after creating a few harmless sample pages. Otherwise start the downloaded executable from its own test-instance directory:

```sh
cd ~/Projects/miniwiki-release-smoke-test/instance
../miniwiki-v0.24.2-beta-linux-amd64/miniwiki serve -addr 127.0.0.1:9090 -db wiki.db
```

Use Firefox to verify the public-facing pages. For a good README screenshot, create a small welcome page and a second linked page, preferably using fictional content.

## Automatic screenshot capture (Firefox or Chromium)

The included helper finds Chromium or Firefox and uses a **fresh headless browser profile** with no logged-in cookies to capture public pages at `/wiki/main-page`, `/pages`, and `/search`:

```sh
cd ~/Projects/miniwiki-beta-source/miniwiki_v24_2
bash scripts/capture_screenshots.sh http://127.0.0.1:9090
```

On Ubuntu, your existing Firefox installation should be sufficient; Chromium also works. The script refuses to run if it cannot reach MiniWiki or detects a redirect to the login page. It creates three real PNG images in `docs/images/` and enables the corresponding README gallery. Review them visually before committing.

If you use `require_login = true`, do not disable authentication on a real private wiki for screenshots. Instead, use a disposable demo instance with public/sample content or take manual screenshots after logging into the demo.

## Firefox alternative

Open the demo wiki in Firefox, right-click the page, choose **Take Screenshot**, select a useful region or the full page, and save it as:

- `docs/images/wiki-home.png`
- `docs/images/all-pages.png`
- `docs/images/search.png`

If you saved images manually, replace the text between `<!-- SCREENSHOT-GALLERY-START -->` and `<!-- SCREENSHOT-GALLERY-END -->` in `README.md` with normal Markdown image references:

```markdown
![MiniWiki home page](docs/images/wiki-home.png)
![MiniWiki page list](docs/images/all-pages.png)
![MiniWiki search](docs/images/search.png)
```

Check image contents and ensure the files are staged before pushing the README change.
