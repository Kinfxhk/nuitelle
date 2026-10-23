# UI review — Flow (self-use build) vs Nuitelle

Flow was reviewed from its source at `~/HermesHub/07-scripts-staging/sandbox/portal-next/frontend/src` because the Linux host (`http://100.118.49.11:8901`) is offline (Tailscale reports last seen 1 day; WOL sent, no response). This is a source-level comparison, not a live screenshot diff.

## Flow shell

- Desktop: fixed left rail (brand, 8 nav items, DS4 status button at the bottom) + top bar (breadcrumb, global search, health pill, emergency STOP).
- Mobile: bottom nav with 4 items + “more” sheet.
- Sections: Home, Library, Discover, Favorites, Channels, Playlists, Tasks, Device.
- Player: full-screen-safe queue, resume, speed, DS4 panel, floating DS4 popover.
- Details: skip link, toast region, playlist picker, breadcrumbs, 15s health polling.

## Nuitelle before this pass

- Single top header; nav items Home / Library / Tags; panels for layout and collections.
- Player modal with tags, trash, subtitles; no queue, no speed, no favourites.
- Layout JSON was already more customisable than Flow (order, visibility, links, theme).

## Gaps found

| Area | Flow | Nuitelle (before) | Action |
|---|---|---|---|
| Navigation shell | desktop rail + top bar | single row | **adopted**: desktop rail + top bar |
| Mobile nav | bottom bar + more | header wraps | **adopted**: five-slot bottom nav |
| Health/status | pill + polling | none | **adopted**: pill + 15s poll |
| Emergency stop | global STOP | none | needs device daemon |
| Favourites | dedicated section | none | **adopted**: nav item + star |
| Queue | next/prev + autoplay | none | **adopted**: prev/next + ended→next |
| Playback speed | yes | none | **adopted**: 0.75–2× |
| Keyboard | partial | none | **adopted**: ←/→, space, f, n |
| DS4 | panel + lease | none | later (`hapticsd`) |
| Playlists / Tasks | full pages | collections only | later |
| Layout customisation | fixed | JSON + theme + links | **Nuitelle is stronger** |

## What was adopted in 0.3.0

- Health pill with online/scanning/offline states.
- Favourites nav entry and a star toggle in the player (stored as the `favorite` tag).
- Previous/next in the player, autoplay next on end, 0.75×–2× speed.
- Keyboard: arrows seek 5s, space toggles, `f` fullscreen, `n` next.
- Bulk selection with tag/trash actions.
- Collections (saved searches), tag rename/merge/alias API.
- Theme controls (accent, density, radius) and custom external links in the rail data.

## Next

1. Desktop rail + mobile bottom nav, driven by `layout.json` (so users can reorder the rail itself).
2. Device page placeholder that documents the future daemon; keep STOP semantics for one writer.
3. Playlists (static + smart) on top of collections.
4. A-B loop and subtitle offset in the player.

The design rule stays: **layout is data, never code**; plugins may only fill declared slots.

## v1.5.0 redesign (2026-10-23)

Full visual overhaul of the shell while keeping layout-as-data and all feature handlers:

- Warm screening-room palette (near-black #0c0a08, amber accent), replacing the GitHub-dark blues.
- SVG icon system (stroke, currentColor) for rail, top bar, mobile nav; zero web-font/asset deps.
- Top bar hierarchy: Scan is the primary filled button; the rest are ghost buttons.
- Poster cards: borderless art, hover accent ring + play glyph, duration/res badges, progress bar with dark track on continue-watching, relative "added" date line.
- Rail: active item uses an accent-tinted gradient; status is a real pill with localized text.
- Player restructure: header (title + close), video, footer split into left info (tags, inline tag input) and right controls (prev/next/speed/subs/favourite/playlist/more); everything else moved into a "more" drawer; trash styled destructive and separated from close.
- Bulk bar: shows only on selection, selection count, select-all, destructive trash, centered on the content column; mobile: full-width sheet above the bottom nav.
- Mobile: single-row scrollable top bar with edge fade, scrolling chip row, larger nav icons, active state on both navs, safe-area padding.
- Bug fixes: `[hidden]` was overridden by class display rules (bulk bar/grid/chips rendered when hidden); nav active state was hard-wired to Home; mobile bottom padding was 14px instead of 88px; greeting punctuation was ASCII in CJK locales; `added_at` (Unix seconds) was parsed as milliseconds.
- Docs screenshots regenerated (same filenames); annotation colour switched to cyan so it no longer clashes with the amber accent.
