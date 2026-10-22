# Roadmap — full self-service media library

Goal: a complete, local-first tool that a non-developer can install in one command, then use as their only video library: import, organise, search, play, subtitle, restore, back up.

## Status

Done through 0.3.x: scan, tags, search, Range streaming, covers, progress, trash/restore, layout JSON + theme + links, 5-language UI, local Whisper subtitles, bulk actions, collections, tag rename/merge/alias, duplicates (size+duration), path remap, library health, queue/autoplay/speed, Docker, installer, CI on macOS+Linux.

## Phases

### P1 — Library that feels complete (next)
- [x] **Bulk select** + bulk tag/trash (restore/rename next)
- [x] **Collections / saved filters** (query saved as JSON)
- [x] **Tag manager API**: rename, merge, aliases (UI + facets next)
- [x] **Duplicate finder** (perceptual hash + duration bucket) + UI cleanup
- [x] **Path remap** API + UI
- [x] **DB health** UI: zero-byte, missing duration, orphan tags, missing-file cleanup
- [ ] **Backup / restore / integrity check** buttons
- [x] Player: queue, autoplay next, speed (A–B, PiP, audio tracks next)
- [x] Keyboard shortcuts + command palette (⌘K)

### P2 — Subtitles and metadata
- [x] Local Whisper transcription (whisper.cpp, model switchable)
- [x] Whisper translate-to-English path
- [x] LLM translation for zh-Hant / zh-Hans / ko / ja (local endpoint, batch)
- [x] Subtitle offset (shift) in the player; merge/split next
- [x] Sidecar discovery + embedded subtitle extraction
- [x] NFO import + local poster discovery (JSON sidecar/fanart next)
- [ ] Performer/studio entities with aliases
- [x] Local AI vision tagging: single + batch + folder, review queue, confidence, accept/reject

### P3 — Import and remote
- [x] Generic ingest: HTTP / HLS (no resume/checksum yet)
- [x] Watch folders (3s debounce; copy-completion detection via scan skip)
- [ ] Browser extension “send to Vela”
- [ ] SMB / WebDAV / mounted cloud folders (read-only)
- [ ] Optional rclone backend
- [ ] Deduplicate on import by hash

### P4 — Extensibility
- [x] JS plugin host MVP (scan.classify, bare VM, 2s limit); WASM next
- [x] JS hooks: scan.classify + metadata.fetch + subtitle.fetch + sandboxed UI panel
- [ ] UI packs (layout + theme + widgets)
- [ ] Plugin registry with signatures and revoke list
- [ ] REST/OpenAPI + WebSocket events

### P5 — Living room and feedback
- [x] TV mode (larger UI, arrow/gamepad navigation)
- [x] Cast button (Remote Playback / AirPlay); DLNA server next
- [ ] Phone as remote
- [ ] `hapticsd`: controller feedback daemon, audio-reactive patterns, latency calibration (protocol details in community docs)
- [x] Multi-profile resume positions + favourites (per-profile auth next)

### P6 — Operations
- [x] LAN mode with random token
- [x] Password login (bcrypt + signed session)
- [x] TOTP second factor (RFC 6238)
- [ ] TLS reverse-proxy guide
- [ ] Scheduled scans and backups
- [ ] Metrics endpoint (local only)
- [ ] ARM/MIPS builds for NAS

## Design constraints

1. One binary; no Go/Node/Python needed by the user.
2. Everything local; no telemetry; loopback by default.
3. Content-neutral public repo: no site extractors, no bundled media, no “supported sites” list.
4. Plugin UI is JSON only; no script injection.
5. Every destructive action is reversible (trash, operation log, undo).

## v1.0 audit (2026-10-22)

Shipped: incremental scan, tags + FTS5, Range streaming, covers, trash/restore, missing-file cleanup, perceptual duplicates, path remap, collections, playlists, bulk actions, favourites, multi-profile progress/favourites, home shelves, command palette, TV mode, five locales, Whisper subtitles + translation, sidecar + embedded subtitles, local VLM tagging with review queue, plugin host (scan/metadata/subtitle/UI), URL ingest, transcode with hardware acceleration, Stash import, JSON export/import, LAN token, password + TOTP, DLNA, hapticsd, Docker, installer, CI, releases.

Remaining (post-1.0): per-profile passwords, plugin registry/signing, macOS/Windows haptics, Bluetooth controllers, DLNA paging beyond 200, watch folders, browser extension, SMB/WebDAV/rclone, trickplay, subtitle merge/split, NFO/fanart, Windows builds, code signing.
