# Nuitelle 1.0 — feature inventory

Local-first personal media library. One Go binary, SQLite, embedded UI, no cloud.

## Library

- Incremental scan, skips unchanged and still-copying files.
- Watch folders: fsnotify with a 3s debounce; `--no-watch` disables.
- NFO (`<name>.nfo`) supplies title/tags; local `<name>-poster.jpg` or `poster.jpg` becomes the cover.
- ffprobe metadata (duration, resolution, codecs), JPEG covers.
- Range streaming of the original file; no transcode by default.
- Trash/restore in place, missing-file detection and cleanup.
- Perceptual-hash duplicate detection (16×16 aHash + duration bucket), size+duration fallback.
- Path remapping after moving a folder/NAS.
- Library health: zero-byte, missing duration, orphan tags, missing files.

## People

- Portrait (3:4) cards or a compact list; Cards/List choice remembered per browser.
- Click a person to filter the library; shift-click adds a second person (OR), with an AND toggle.
- Upload or drag-drop a portrait; re-encoded to 1600 px with a 320 px thumbnail.
- Aliases, counts, sort by name/count/recent, create-on-type.
- Player People row, bulk assignment, merge by CLI, and `import-stash --people` / `people --from-tags`.

## Organisation

- Tags (add/remove, rename, merge, alias, delete), FTS5 trigram search.
- People (performers): portrait cards or a compact list, user-supplied portraits re-encoded locally (1600px + 320px thumb), and person filters on the library grid.
- Collections (saved searches), playlists (drag reorder, remove), favourites.
- Bulk actions: tag, trash, restore, AI tag, translate, transcode.
- Home shelves: time-of-day greeting, continue watching, recently added, favourites.
- Multiple profiles with independent resume positions and favourites.

## Playback

- Queue, autoplay next, 0.75–2× speed, A-B loop, resume, timestamp markers.
- Muted hover previews, one-click bookmark stars, 120-item pages with Load more, trash page with restore.
- Subtitles: sidecar (`.srt`/`.vtt`/`.ass`), embedded tracks, Whisper transcripts, translations.
- Subtitle offset, track selector with source labels; markers jump/delete in the player.
- Cast button (Remote Playback/AirPlay), original-file download.
- Keyboard shortcuts and command palette (`⌘K` / `Ctrl+K`).
- TV mode: larger UI, arrow/gamepad focus navigation.

## Local AI / speech

- Whisper.cpp transcription, switchable models (tiny → large-v3), local only.
- English translation via Whisper; other languages via a local OpenAI-compatible LLM.
- Vision tagging with a local VLM (Qwen3-VL 2B/4B tested): one item, a selection, or a folder.
- Suggestions carry confidence and go through a review queue; nothing is written silently.

## Input / output

- URL ingest: direct http(s) file or HLS `.m3u8`, into `_ingest/`.
- Transcode: H.264/AAC, auto VideoToolbox/NVENC/VAAPI, else libx264, into `_transcoded/`.
- Metadata export/import as JSON (no media), merge-safe and atomic (one transaction; failure rolls back).
- Flow import: paths, progress, favourites, active playlists, trash state.
- Stash import: titles, tags, performers, studio names, matched by path.

## Customisation

- `layout.json`: sidebar order, visibility, custom external links, per-device layouts.
- Theme: accent, density (compact/comfortable/roomy), radius, TV mode.
- Presets: Simple (Home/Library/Favourites) and Full (+Tags).
- Settings split into Basic and Advanced tabs, keeping the daily controls first; experimental toggles hide cast/haptics/marker controls.
- Five locales: English, 繁體中文, 简体中文, 한국어, 日本語.
- Plugin host (JS, goja): `scan.classify`, `metadata.fetch`, `subtitle.fetch`, sandboxed `ui` panel.
- Plugin scripts have no filesystem, network, or timers; 2s interrupt.

## Discover / automation

- Explore: random picks and unwatched per profile.
- Sources: RSS/Atom feeds; media enclosures are ingested into `_ingest/` and indexed; tests with a local feed.
- Tasks: persistent jobs with progress, per-kind state and cancel for batch AI.
- Device: hapticsd status, intensity and a test pulse.

## Devices

- Desktop rail, mobile bottom navigation, PWA-friendly layout.
- LAN mode with a random token (opt-in); password login and TOTP optional.
- DLNA MediaServer (`--dlna`): SSDP + ContentDirectory, read-only, port 8772, bound to the LAN IP with a loopback copy for local tests.
- `hapticsd`: WebSocket daemon, Linux USB DualShock 4 / DualSense rumble, mock driver, player toggle + intensity.

## Security

- Binds loopback by default; LAN is explicit.
- Host whitelist, Origin checks, `text/plain` write rejection (CSRF/DNS rebinding).
- Path containment via `filepath.Rel` after symlink resolution.
- Layout schema validation; links restricted to http(s); `nosniff`; API 404s are real 404s.
- bcrypt password + HMAC session cookie; TOTP (RFC 6238).
- Plugin iframes use `sandbox allow-scripts` + CSP sandbox.
- URL ingest blocks private/loopback targets by default (`ingest_allow_private` to opt in).
- Login rate limit (5 failures → 5 min), plugin call-stack cap, 2s interrupt.
- No telemetry, no accounts, no cloud calls.

## Operations

- One binary + Docker image; installer script; `nuitelle doctor`.
- Persistent job table; batch AI jobs show progress and can be cancelled.
- Multi-row writes (tags, playlist items, AI suggestions) run in a single transaction.
- `nuitelle backup` writes a consistent SQLite snapshot (refuses to overwrite).
- GitHub Actions: macOS + Ubuntu tests, Linux smoke test (ffmpeg, scan, serve, Range 206).
- Release assets: `nuitelle` and `hapticsd` for linux/darwin/windows × amd64/arm64.
- AGPL-3.0-or-later; plugin SDK/protocol documented in the README.

## Known gaps (post-1.0)

- Per-profile passwords (global auth only).
- Plugin registry and signatures; metadata provider plugins beyond the current hooks.
- macOS/Windows haptics, Bluetooth controller output, real-hardware validation.
- DLNA paging beyond 200 items; no transcoding profile negotiation.
- Watch folders, browser extension, SMB/WebDAV/rclone backends.
- Trickplay sprites, subtitle merge/split, NFO/fanart import.
- Windows builds; no code signing/notarisation yet.
