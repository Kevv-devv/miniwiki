# MiniWiki v19 Security and Moderation Pass

## OWASP Top 10 review

This version adds a practical OWASP Top 10 review and hardening pass for the current MiniWiki alpha.

### A01 Broken Access Control
- Admin-only routes are checked for user, role, and method.
- Blocked users cannot authenticate and existing sessions for blocked users are invalidated.
- Page and upload delete/undelete/protect actions are admin-only.
- Editors/admins can rename uploads; protected uploads cannot be renamed or deleted by non-admins.

### A02 Cryptographic Failures
- Passwords use bcrypt.
- Password reset tokens are random, expiring, and single-use.
- Cookies should be served over HTTPS in production through a reverse proxy.

### A03 Injection
- SQLite calls use parameterized queries.
- Templates use html/template escaping.
- Wiki links, image links, and filenames are escaped/sanitized.

### A04 Insecure Design
- Admin dashboard consolidates sensitive functions.
- Deleted pages/files are soft-deleted so admins can review and undelete.
- Watchlists are available to logged-in users.

### A05 Security Misconfiguration
- Added common HTTP hardening headers: nosniff, frame denial, referrer policy, permissions policy, and a conservative CSP.
- Uploads reject risky active-content file types such as HTML, SVG, JavaScript, and CSS.

### A06 Vulnerable and Outdated Components
- `go mod tidy` and dependency review should be part of every release.
- GitHub Actions should eventually run tests and vulnerability scans.

### A07 Identification and Authentication Failures
- Login throttling is present.
- Password reset avoids account enumeration.
- Admin password resets are audited.

### A08 Software and Data Integrity Failures
- Release engineering still needs signed/checksummed archives in a later version.
- Embedded assets reduce runtime template tampering.

### A09 Security Logging and Monitoring Failures
- Audit logging covers login failures, CSRF failures, page/file moderation, CheckUser views, account updates, and user administration.

### A10 SSRF
- MiniWiki does not fetch remote URLs as part of normal wiki rendering or uploads.

## User-facing changes
- Cleaned up user settings page.
- Added common edit-summary suggestions.
- Confirmed watchlists are available from page actions and the navbar.
- Admins can block/unblock users.
- Admins can delete/undelete pages and files.
- Admins can view deleted pages and deleted files without restoring them.
