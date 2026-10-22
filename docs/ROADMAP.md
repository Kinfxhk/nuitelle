# Roadmap — full self-service media library

Goal: a complete, local-first tool that a non-developer can install in one command, then use as their only video library: import, organise, search, play, subtitle, restore, back up.

## Status

Done through 0.3.x: scan, tags, search, Range streaming, covers, progress, trash/restore, layout JSON + theme + links, 5-language UI, local Whisper subtitles, bulk actions, collections, tag rename/merge/alias, duplicates (size+duration), path remap, library health, queue/autoplay/speed, Docker, installer, CI on macOS+Linux.

## Phases

### P1 — Library that feels complete (next)
- [x] **Bulk select** + bulk tag/trash (restore/rename next)
- [x] **Collections / saved filters** (query saved as JSON)
- [x] **Tag manager API**: rename, merge, aliases (UI + facets next)
- [x] **Duplicate finder** (size+duration) + UI cleanup (perceptual hash next)
- [x] **Path remap** API + UI
- [x] **DB health** UI: zero-byte, missing duration, orphan tags (filesystem scan next)
- [ ] **Backup / restore / integrity check** buttons
- [x] Player: queue, autoplay next, speed (A–B, PiP, audio tracks next)
- [x] Keyboard shortcuts + command palette (⌘K)

### P2 — Subtitles and metadata
- [x] Local Whisper transcription (whisper.cpp, model switchable)
- [x] Whisper translate-to-English path
- [ ] LLM translation for zh-Hant / zh-Hans / ko / ja (local endpoint)
- [x] Subtitle offset (shift) in the player; merge/split next
- [x] Sidecar discovery (`.srt`, `.vtt`, `.ass`); embedded extraction next
- [ ] NFO / JSON sidecar import, poster/fanart discovery
- [ ] Performer/studio entities with aliases
- [x] Local AI vision tagging: single + batch + folder, review queue, confidence, accept/reject

### P3 — Import and remote
- [ ] Generic ingest queue: HTTP / HLS / DASH, resume, checksum
- [ ] Watch folders with copy-completion detection
- [ ] Browser extension “send to Vela”
- [ ] SMB / WebDAV / mounted cloud folders (read-only)
- [ ] Optional rclone backend
- [ ] Deduplicate on import by hash

### P4 — Extensibility
- [x] JS plugin host MVP (scan.classify, bare VM, 2s limit); WASM next
- [ ] Metadata provider / subtitle provider / theme plugins
- [ ] UI packs (layout + theme + widgets)
- [ ] Plugin registry with signatures and revoke list
- [ ] REST/OpenAPI + WebSocket events

### P5 — Living room and feedback
- [ ] TV mode (10-foot UI, gamepad navigation)
- [ ] Chromecast / AirPlay
- [ ] Phone as remote
- [ ] `hapticsd`: controller feedback daemon, audio-reactive patterns, latency calibration (protocol details in community docs)
- [ ] Multi-user profiles (optional, local)

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
