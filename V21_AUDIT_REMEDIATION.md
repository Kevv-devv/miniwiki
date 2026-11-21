# MiniWiki v21 — v20 audit remediation record

Source of the checklist: `miniwiki_v20_audit.md` supplied by the project owner.

| Finding | v21 response |
|---|---|
| H1 deleted content through revision/diff/compare/recent | Public storage Revision, PrevRevision, Revisions, Recent now filter out `pages.deleted_at`. Only dedicated admin deleted-page view remains. |
| H2 unsafe WAL backup/restore; missing uploads | New offline `backup` and `restore` commands use SQLite VACUUM INTO, locked directory snapshots including uploads, SQLite integrity checking, pre-restore rollback snapshot, and cleanup of old WAL/SHM. v20 DB-only backups deliberately not accepted. |
| H3 admin/admin123 recreated on boot | Seed creates admin only when users table is empty, generates a secure random temporary password (optional environment override), requires first-login password change, never force-promotes username admin; one-time upgrade detects and rotates surviving known v20 password. |
| H4 session persistence after password changes | Session IDs are hashed in database; self-change, admin reset and token reset all revoke sessions in same transaction. Pre-v21 sessions revoked once at migration. |
| M1 missing server timeouts | ReadHeader, Read, Write, Idle timeout configured on `http.Server`. |
| M2 forwarded IP header variations and /0 networks | Join all XFF header lines; trust only explicitly allowed proxy peers; reject `/0` networks. |
| M3 GET logout CSRF | POST-only logout, CSRF validated, navbar form. |
| M4 account enumeration / SMTP blocking | Bcrypt dummy comparison for unknown/blocked users; generic forgot-password responses; queued asynchronous SMTP delivery with connection deadlines and STARTTLS checks. |
| M5 backlinks broken by renames | Resolve stored old link targets via redirects when querying backlinks, including after index rebuild. |
| M6 edits overwrite each other | Revision-token optimistic concurrency inside SQLite transaction; HTTP 409 preserves draft text. |
| M7 inconsistent noncanonical slugs | Canonical redirect on GET for titles using raw spaces/capital letters, reject noncanonical POST, canonical internal slugs unchanged. |
| M8 oversized pages | 2 MiB maximum enforced in handler and storage layer; bounded POST request body. |
| M9 admin self/last-admin role changes | Self-demotion rejected by handler; last-active-admin demotion and block rejected transactionally in storage. |
| P1 private wiki option | New `require_login`/`MINIWIKI_REQUIRE_LOGIN`, applying to all page/revision/history/search/upload reads. |
| P2 redirect notice and loops | Final-target resolution with maximum hop count and cycle detection; shows “Redirected from…” with an inspectable source link. |
| P3 positional/no-op migrations and rebuild every boot | Safe `ensureColumn` checks, explicit versioned 21001 migration marker, one-time rebuild of FTS/page links and session rotation; baseline `CREATE IF NOT EXISTS` statements remain idempotent. |
| P4 phrase-only FTS | Whitespace-separated token AND search with last-token prefix matching. |
| L1 link placeholder collision | Cryptographically random per-render token prefix. |
| L2 emails unvalidated/ambiguous | ParseAddress validation, lowercase normalization, partial unique index on nonempty emails; existing duplicates must be resolved before migration. |
| L3 plaintext session IDs | SHA-256-hashed opaque identifiers stored in SQLite, old session rows revoked on upgrade. |
| L4 orphan uploads / XHTML/XML | Failed database insert removes the physical upload; XML and XHTML extensions rejected. |
| L5 user/IP spray throttling | Added additional per-IP failure limit beside per-username/IP window. |
| L6 go.sum/CI/vuln scanning | Added GitHub Actions tests, race detector, go vet, govulncheck and cross-platform compilation. `go.sum` cannot be produced in this offline build environment; must be generated with `go mod tidy` online and committed. |

## Verification boundaries

The source passed Go parser/formatter and was compiled using *temporary local dependency stubs outside the deliverable* for basic type checking. These stubs are not functional implementations and cannot validate behavior. **Full Go tests, SQLite backup correctness, actual SMTP delivery, and third-party dependency vulnerability results remain unverified in this offline environment.** The real repository CI and the Ubuntu commands in `docs/UPGRADING_V20_TO_V21.md` must be run before trusting the release publicly. Added regression tests specifically cover deleted revisions, role guards, password/session changes, edit conflicts, proxy headers, redirects, private wiki reads, and WAL snapshots.

Do not claim this source has undergone a penetration test. Before public exposure, enable HTTPS, secure cookies, private mode if desired, make a fresh full backup, and run the GitHub Actions CI results.
