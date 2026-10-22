# Vela

**A local-first personal media library.** Point Vela at folders you own; it indexes them, gives you a fast web UI, and streams the original files without re-encoding.

Vela is content-neutral: it does not host, index, or supply media. You bring your own files.

```
vela init      # choose folders and a port
vela scan      # build the index
vela serve     # open http://127.0.0.1:8770
```

## Features

- **Fast indexing** — incremental scan, skips unchanged files, ignores half-copied files.
- **Streaming that just works** — HTTP Range on the original file; no transcode by default.
- **Tags, search, filters** — SQLite FTS-ready schema, tag chips, instant search.
- **One-click care** — add/remove tags, resume position, move to trash, restore.
- **Customizable UI** — reorder the sidebar in the UI; the layout is stored as JSON and can be exported/shared.
- **Small footprint** — one Go binary, SQLite, optional ffmpeg for covers and metadata.
- **Private by default** — binds `127.0.0.1`, no telemetry, no accounts, `X-Robots-Tag: noindex`.

## Install

### One command (Linux / macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/Kinfxhk/vela/main/install.sh | sh
```

The script downloads the release binary for your OS/arch into `~/.local/bin`. Read it first if you prefer:

```sh
curl -fsSL https://raw.githubusercontent.com/Kinfxhk/vela/main/install.sh -o install.sh
less install.sh
sh install.sh
```

### Docker

```sh
docker compose up -d
```

Then open <http://127.0.0.1:8770>. Edit `docker-compose.yml` to mount your folders under `/media`.

### From source

```sh
git clone https://github.com/Kinfxhk/vela
cd vela
go build -o vela .
./vela init && ./vela scan && ./vela serve
```

Requires Go 1.23+. ffmpeg/ffprobe are optional but recommended.

## Requirements

- macOS 12+ (Intel or Apple Silicon), Linux x86_64/arm64, or Docker.
- Optional: `ffmpeg` and `ffprobe` for thumbnails and duration/resolution metadata. Playback works without them.

## Configuration

`vela init` writes:

- macOS: `~/Library/Application Support/vela/config.json`
- Linux: `~/.config/vela/config.json`

```json
{
  "roots": ["/Volumes/Media/Videos"],
  "addr": "127.0.0.1:8770",
  "db": ".../vela.db",
  "cover_dir": ".../covers"
}
```

Run `vela doctor` to check folders, ffmpeg, database, and port.

## UI customization

The sidebar order is stored as JSON (`GET/PUT /api/layout`). Use **Layout** in the header to reorder, or edit the JSON directly:

```json
{
  "version": 1,
  "sidebar": [
    {"id": "home", "label": "Home", "route": "/", "order": 10},
    {"id": "library", "label": "Library", "route": "/library", "order": 20},
    {"id": "tags", "label": "Tags", "route": "/tags", "order": 30}
  ]
}
```

Layout files are portable between machines.

## API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | server + scan state |
| GET | `/api/stats` | files, bytes, duration, tags |
| GET | `/api/media?q=&tag=&sort=&limit=&offset=` | list/search |
| GET | `/api/media/{id}` | detail + resume position |
| GET | `/api/media/{id}/stream` | Range streaming |
| GET | `/api/media/{id}/cover` | JPEG thumbnail (ffmpeg) |
| POST | `/api/media/{id}/tags` | `{"add":[],"remove":[]}` |
| POST | `/api/media/{id}/progress` | `{"position_ms":1234}` |
| POST | `/api/media/{id}/trash` | move file to `.vela-trash/` |
| POST | `/api/media/{id}/restore` | move it back |
| POST | `/api/scan` | `{"full":false}` background scan |
| GET/PUT | `/api/layout` | UI layout JSON |

## Roadmap

- optional local AI tagging (vision model runs on your machine; suggestions require review)
- plugin host with a narrow, sandboxed API
- haptic feedback companion (controller/device), separate process
- TV mode, cast, multi-user profiles

No dates. Issues and discussions are welcome.

## Privacy

- No cloud, no telemetry, no accounts.
- Binds loopback only. LAN access is opt-in and prints a warning.
- Media never leaves your machine; the database stores paths and metadata only.

## License

AGPL-3.0-or-later. See [LICENSE](LICENSE).

The name and logo are not covered by the code license; forks may not use them.
