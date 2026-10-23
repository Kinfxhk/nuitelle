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
