# MiniWiki v21.1 — First-run administrator regression-test fix

## Issue corrected

On a newly seeded wiki, the first administrator has `must_change_password=1`.
The security middleware correctly redirects requests to `/admin` to `/account`
until the administrator changes their initial temporary password.
The v21 admin endpoint test omitted the required first-login step and therefore
failed with HTTP 303 instead of receiving the dashboard.

## Fix

The handler regression test now exercises the intended first-login workflow:

1. Verifies that the initial administrator cannot visit `/admin` yet.
2. Verifies that `/account` shows the mandatory change-password instruction.
3. Changes the password through the actual HTTP POST route with CSRF protection.
4. Verifies that this revokes the old session and allows authentication with the new password.
5. Creates a fresh authenticated session and tests admin audit/password-reset operations.

No security policy or application functionality was relaxed. The only runtime
source-code change is the version label, from 0.21.0-dev to 0.21.1-dev.

## Testing

Run `go mod tidy` and then `go test ./...` on Ubuntu. This build environment
does not have access to real Go modules, so passing the full integration tests
cannot be certified here. Offline compilation with substitute dependency stubs
is NOT equivalent to running the real integration suite.

## Existing wiki data

Stop v21 completely before moving database and uploads. Copy files from the
v21 folder into v21.1, not from the older v20 folder; otherwise you could lose
pages created since the last upgrade. See the installation commands supplied
with this archive.
