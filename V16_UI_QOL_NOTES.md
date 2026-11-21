# MiniWiki v16 Quality-of-Life Pass

Implemented on top of the verified v13.2/v14 line.

## Added

- Page rename for admins at `/wiki/{slug}/rename`.
- Automatic redirects from old page slugs to new page slugs.
- Upload gallery cards with image previews.
- Copy-paste Markdown snippets for uploaded images and files.
- Drag-and-drop upload UI.
- Keyboard shortcuts: `E` to edit, `/` to search, `R` for recent changes.
- Search snippets render FTS `<mark>` highlights safely by escaping user content first.
- Additional dark-mode and layout polish.

## Notes

Uploads remain stored in the configured upload directory. Images can be embedded in pages with normal Markdown image syntax, for example:

```markdown
![Alt text](/uploads/example.png)
```
