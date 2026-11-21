# MiniWiki v14 UI/UX Pass

This release focuses on the day-to-day writing and reading experience.

## Added

- Responsive top navigation with a mobile menu toggle.
- Dark mode toggle stored in browser localStorage.
- Edit toolbar for common Markdown/wiki syntax:
  - bold, italic, heading, external link, wiki link, category, quote, code block.
- Live client-side draft preview in the editor.
- Word and character count in the editor.
- Sticky edit action bar for Save / Preview / Cancel.
- Automatic table of contents on page view, generated from headings.
- Improved article layout, spacing, typography, cards, diff styling, empty states, and mobile layout.

## Notes

The live preview is intentionally approximate. The authoritative render still happens on the server through Goldmark and MiniWiki's wiki-link/category renderer. Use the Preview button for the server-rendered version before saving if exact output matters.
