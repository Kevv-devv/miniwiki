# MiniWiki v17 Feature Notes

This release focuses on wiki governance, deleted-content recovery, MediaWiki-like redirects, account moderation, CheckUser-style audit visibility, and easier image use inside articles.

## Implemented

- Pages are soft-deleted instead of physically removed.
- Admins can list deleted pages, view deleted page content without undeleting, and undelete pages.
- Pages whose entire content is a single wiki link, such as `[[Statistics]]`, now redirect to the target page. Add `?redirect=no` to view the redirect page itself.
- Admins can block and unblock accounts. Blocking removes existing sessions and prevents new login.
- Admins can use CheckUser to review audit activity and recorded IP information for a username.
- `[[Image:name]]` / `[[File:name]]` embeds an uploaded image inline.
- Plain `[[name]]` remains a normal wiki link, as requested.
- Editors/admins can rename uploads.
- Admins can delete, undelete, protect, and unprotect uploads/images.

## Notes

The file bytes remain on disk when an upload is soft-deleted, but MiniWiki stops serving deleted uploads through `/uploads/`.
