# Flow → Nuitelle mapping

Flow (the old private build) and Nuitelle are both media libraries, but Flow optimised for one personal catalogue; Nuitelle is content-neutral and safe to publish. This is the section-by-section mapping.

| Flow | Nuitelle | Notes |
|---|---|---|
| 首頁 Home | **Home** | Continue watching, recently added, favourites; adds a time-of-day greeting. |
| 媒體庫 Library | **Library** | Grid + filters + FTS search, 120-item pages with Load more, hover previews, card bookmark. |
| 探索 Discover | **Explore** | Random picks + unwatched; no bundled site catalogue. |
| 收藏 Favourites | **Favourites** | Per-profile favourites with a one-click star. |
| 頻道 Channels | **Sources** | RSS/Atom feeds; enclosures that point at mp4/m3u8 are ingested into `_ingest/` and indexed. No site-specific scrapers. |
| 播放清單 Playlists | **Playlists** | Create, drag-reorder, remove, bulk add. |
| 任務 Tasks | **Tasks** | Persistent jobs with progress; batch AI jobs can be cancelled. |
| 裝置 Device | **Device** | `hapticsd` status, intensity and a test pulse (WS 8771). |
| Playback sessions / leases | per-profile progress | Single-user simplification; resume works across devices. |
| Catalog/discover sources |—| Intentionally not shipped: a public repo must not embed catalogues of infringing sites. Use plugins/metadata hooks instead. |
| DS4 writer on 8765 | `hapticsd` + `/api/haptic` proxy | Browser talks to the Nuitelle server, which proxies to hapticsd; no 8765 bridge needed when the old UI is retired. |
| HLS playback | direct play + transcode jobs | Direct Range streaming first; hardware transcode writes a compatible copy. |

## Migration commands

```sh
# Metadata (paths, progress, favourites, active playlists, trash state)
nuitelle import-flow --db ~/portal-next/data/flow.sqlite3

# Optional: Stash performers become first-class People
nuitelle import-stash --db ~/.stash/stash-go.sqlite --people
```

The importer skips rows whose files no longer exist and reports `missing=`.
