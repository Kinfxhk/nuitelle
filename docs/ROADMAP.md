# Roadmap — full self-service media library

Goal: a complete, local-first tool that a non-developer can install in one command, then use as their only video library: import, organise, search, play, subtitle, restore, back up.

## Status

Done in 0.1.x: scan, tags, search, Range streaming, covers, progress, trash/restore, layout JSON, 5-language UI, local Whisper subtitles, Docker, installer, CI on macOS+Linux.

## Phases

### P1 — Library that feels complete (next)
- [ ] **Bulk select** + bulk tag/trash/restore/rename (one-click care)
- [ ] **Collections / saved filters** (smart: query + sort, shareable JSON)
- [ ] **Tag manager**: rename, merge, aliases, facet groups, colour
- [ ] **Duplicate finder** (perceptual hash), merge duplicates
- [ ] **Path remap** wizard after moving a NAS/folder
- [ ] **DB health**: missing files, orphans, zero-byte, probe failures
- [ ] **Backup / restore / integrity check** buttons
- [ ] Player: queue, autoplay next, A–B loop, speed, PiP, audio tracks
- [ ] Keyboard shortcuts + command palette (⌘K)

### P2 — Subtitles and metadata
- [x] Local Whisper transcription (whisper.cpp, model switchable)
- [x] Whisper translate-to-English path
- [ ] LLM translation for zh-Hant / zh-Hans / ko / ja (local endpoint)
- [ ] Subtitle editor (shift, merge, split, re-time)
- [ ] Sidecar discovery (`.srt`, `.vtt`, `.ass`) + embedded track extraction
- [ ] NFO / JSON sidecar import, poster/fanart discovery
- [ ] Performer/studio entities with aliases
- [ ] Local AI vision tagging: folder or single video, suggestions + confidence + accept/reject

### P3 — Import and remote
- [ ] Generic ingest queue: HTTP / HLS / DASH, resume, checksum
- [ ] Watch folders with copy-completion detection
- [ ] Browser extension “send to Vela”
- [ ] SMB / WebDAV / mounted cloud folders (read-only)
- [ ] Optional rclone backend
- [ ] Deduplicate on import by hash

### P4 — Extensibility
- [ ] WASM plugin host (wazero) with narrow API
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
- [ ] Optional auth (password + TOTP), TLS reverse-proxy guide
- [ ] Scheduled scans and backups
- [ ] Metrics endpoint (local only)
- [ ] ARM/MIPS builds for NAS

## Design constraints

1. One binary; no Go/Node/Python needed by the user.
2. Everything local; no telemetry; loopback by default.
3. Content-neutral public repo: no site extractors, no bundled media, no “supported sites” list.
4. Plugin UI is JSON only; no script injection.
5. Every destructive action is reversible (trash, operation log, undo).
