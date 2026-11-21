# MiniWiki v0.24.2-beta-rc — dependency security remediation

This is a targeted follow-up to the first full Ubuntu `govulncheck` run for v0.24.1-beta. That scan stopped the original upgrade **before** the database backup/restore steps, with two *reachable* findings:

| Advisory | Dependency before | Pinned in v24.2 | Why it matters |
|---|---|---|---|
| GO-2026-5320 | github.com/yuin/goldmark v1.7.4 | v1.7.17 | Prevents XSS via character-reference-encoded dangerous Markdown links and images. |
| GO-2026-5970 | golang.org/x/text v0.17.0 | v0.44.0 | Prevents a Unicode normalization infinite loop on invalid UTF-8. |

Official advisories: <https://pkg.go.dev/vuln/GO-2026-5320> and <https://pkg.go.dev/vuln/GO-2026-5970>.

The `go` directive is set to `1.26.0` (required by x/text v0.44.0) and the preferred `toolchain` is `go1.26.11`, which the Ubuntu scanner already fetched. The newer x/text pin also covers the later 2026 advisory GO-2026-6629 (fixed in v0.41.0). This deliberately leaves the other direct dependency versions unchanged to avoid broad, unnecessary changes during a security hotfix. `go mod tidy` may update the transitive dependency graph.

## Added regression tests

- `internal/render/security_v24_2_test.go`: character-reference-encoded dangerous scheme; dangerous autolink; dangerous image URL.
- `internal/slug/security_v24_2_test.go`: malformed UTF-8 remains bounded during Unicode slug processing.

## Verification and release gates

The Go source and module manifest were inspected and the new files were formatted. **The full test suite and a post-upgrade vulnerability scan have NOT been run in the packaging environment** because it cannot resolve the Go module proxy. Run the steps in `docs/UPGRADING_V24_TO_V24_2.md` on Ubuntu, and do not tag the beta if the scan finds another reachable vulnerability. The old scan also reported 24 vulnerabilities in required modules at symbols MiniWiki did not appear to call; those are not equivalent to the two reachable findings and may remain after this targeted update. Review the entire scanner output.

The archive does **not** include a verified `go.sum`; copy the locally generated file from the previously tested installation before `go mod tidy`, then retain and commit the updated `go.sum` with the release. CI intentionally refuses a missing `go.sum`.

No database schema or application storage migration was added in this hotfix; the version string is `0.24.2-beta-rc`.
