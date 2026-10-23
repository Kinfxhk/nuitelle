# Sorting — where it matters

Audit of every list surface and the sort keys that make sense. Existing keys are marked ✓.

| Surface | Useful keys | Notes |
|---|---|---|
| Library grid | Newest ✓ · Oldest · Title A–Z ✓ · Title Z–A · Longest ✓ · Shortest · Largest ✓ · Smallest · Resolution · Last played · Random | Default: newest. Persist per browser. |
| Favourites | Recently favourited · Title · Longest · Largest · Random | Order by `profile_favorites.created_at`. |
| Trash | Recently trashed · Title · Largest | No trash timestamp yet; use `added_at` until a column is added. |
| People | Name ✓ · Video count ✓ · Recently added ✓ · Random · Last played | Last played requires a join on progress. |
| Playlists (panel) | Manual ✓ · Name · Created · Item count | Panel currently fixed by creation. |
| Playlist items | Manual ✓ · Title · Longest · Recently added · Random | Manual remains the default; touch ↑↓ keeps working. |
| Explore | Fixed rows (random + unwatched) | Add a shuffle action rather than a sort. |
| Catalog results | Relevance · Title · Year | Provider order is usually relevance. |
| Wishlist | Recently added · Title · Year | |
| Tasks | Updated · State | Mostly chronological already. |
| Search results | Relevance · Newest · Title | Default relevance when `q` is present; newest otherwise. |

## Rules

1. **One control per view**, in the view toolbar, not in global settings.
2. **Persist per view** in `localStorage` (`nuitelle.sort.<view>`).
3. **Server-side sort** for anything that can exceed one page; never sort only the current page client-side.
4. **Stable tie-breaker**: always append `title` (or `id`) so pagination does not shuffle equal keys.
5. **Random** must be seeded per session, otherwise “load more” repeats items.

## Implementation notes

- `MediaFilter` gains `Profile` so `last_played` can join `progress` for the current profile.
- `ListFiltered` switch: `new, old, name, name_desc, size, size_asc, duration, duration_asc, resolution, mtime, last_played, random`.
- Favourites and trash take a `sort` parameter; people gains `random`.
- UI: a single `<select>` rendered per view; library/favourites/trash share the same component with different option sets.
