# Media (M13) – design

Implemented in `internal/media` (service, permissions, delivery, image editing, watermarks), `internal/server/api_media.go`, `internal/cli/media.go`, `web/src/components/media.ts` (library, upload, details, image editor, display, selection) and the macro `gallery` (`plugins/core_macros`). Plugins reach the service through `sdk.Host.Media`, in the frontend through `pickMedium`, `uploadDialog`, `mediaURL`, `mediaViewer` from `@ewiki/sdk`. Open: registration of viewer programs (with M14).

## Data model (site schema)

- `categories.hidden boolean` – hidden categories do not appear in navigation, menus, lists and search, but they are normal categories with permissions (freely manageable "folders", e.g. for media).
- `media` – one medium: `id`, `category_id`, `name`, `title`, `alt`, `description`, `current` (current version), `download` (download allowed), `own_acl` (own permissions instead of category/embedding), `watermark jsonb` (`mode`, `user`, `date`, `comment`), `created_by`, `created_at`, `updated_at`, `deleted_at`.
- `media_versions` – `media_id`, `version`, `hash` (file in storage), `mime`, `size`, `width`, `height`, `ops jsonb` (editing steps), `note`, `created_by`, `created_at`. Every edit in the image editor is a new version; embeddings always show the current one.
- `media_acl` – own permissions of a medium: `subject_type` (user, group, role, guest, authenticated), `subject_id`, `perms` (bits: view 1, download 2, edit 4, manage 8).
- `media_usage` – (`media_id`, `element_id`): determined from the content when an element is saved (references `/api/v1/media/{id}/…` and macros).

## Permissions (order)

1. `own_acl`: only `media_acl` counts (narrower or wider than the category; you can only grant permissions you have yourself).
2. Otherwise: whoever may view the category **or** an element in which the medium is embedded may view it; downloading additionally only when `download` is set. Edit/manage: edit permission in the category.
3. Administrators may do everything.

`GET /api/v1/files/{hash}` then only delivers: profile photos, images from the settings (logo etc.), files of media the caller may see, and attachments whose element the caller may see. Everything else: 404.

## Delivery

- `GET /api/v1/media/{id}` (metadata), `/api/v1/media/{id}/view` (display), `/api/v1/media/{id}/download`, `?v=` for older versions (only with edit permission), `?w=` for scaled-down images.
- View only (no download permission or `download=false`): no download button, no direct link, `Content-Disposition: inline`, `Cache-Control: no-store`, context menu and dragging blocked in the frontend. PDFs are shown page by page as images (poppler `pdftoppm`), so no original file ends up in the browser.
- Watermarks (`watermark.mode`):
  - `none`
  - `uploader_only` – the original is not stored, only the version with the uploader's watermark
  - `uploader_view` – display with the uploader's watermark, original for download for those allowed
  - `viewer_view` – the viewer's name (and date) is burnt in when viewing
  - `viewer_all` – when viewing and downloading
  Images: burnt in (`media.Watermark`); PDFs: pages as images with watermark, download as PDF made from these pages. Versions per viewer are cached (key: version, user, day).

## Viewer programs (office)

Office plugins register a `media.Opener` in `Setup` (`host.Media.RegisterOpener`): `ID`, `Label`, `Caps`
(`Restrict`: can block download/print/copy, `Watermark`: overlays text, `Script`: the frontend loads a script),
`Supports(name, mime)` → view/edit, `Launch` → `media.Action` (how the browser starts it) and `Origins` (for the
CSP). Optional: `OptionsProvider` (e.g. read-only without download), `ProofVerifier` (WOPI proof keys),
`FileInfoExtender` (own CheckFileInfo fields).

For WOPI programs there is `pkg/wopi`: discovery (cache, key rotation), proof key checks and `wopi.Opener`,
which is a complete `media.Opener` – a WOPI plugin only needs settings and names (see
`plugins/office_collabora`). The core provides the WOPI host (`/api/v1/wopi/files/{id}`: CheckFileInfo, GetFile, PutFile, Lock,
GetLock, RefreshLock, Unlock).

The plugin registers its own endpoints for office servers (e.g. the OnlyOffice API) with `host.Media.RegisterTokenPath`:
there the core does not evaluate sessions; the handler calls `host.Media.TokenContext(ctx, id, token)` (checks
key and permissions) and then `Content` or `SaveFromOffice`.

The core decides program and mode (`modesFor`): never more than the permissions of the file, watermarks only
protected, without download only with `Restrict` and the option. Access keys: `media_access_tokens` (SHA-256),
locks: `media_locks`. In the browser `registerOfficeRenderer(kind, fn)` starts the program (`wopi` in the core,
`onlyoffice` in the plugin), the page `/office/{id}?viewer=…&mode=…` shows it.

## Interface, CLI, API

- Media library (category tree including hidden ones, grid/list, search, filter by type), upload with permissions, download switch and watermark options, detail view with versions and usages, image editor.
- Inserting in the editor through the media library (also uploading directly there, default: the category of the page).
- Macro `gallery` (media of a category or selected ones, lightbox, keyboard, accessible).
- CLI: `ewiki media list|upload|info|versions|edit|delete|acl|viewers|programs|open`.
