# Vela

**自己的影片，自己管理。** 本地優先、單一二進位檔、無遙測、無帳號。
**Your videos, your library.** Local-first, one binary, no cloud, no accounts.

[繁體中文](#繁體中文) · [English](#english) · [Roadmap](docs/ROADMAP.md) · [Tag design](docs/TAGS.md)

[![release](https://img.shields.io/github/v/release/Kinfxhk/vela?display_name=tag)](https://github.com/Kinfxhk/vela/releases)
[![ci](https://github.com/Kinfxhk/vela/actions/workflows/ci.yml/badge.svg)](https://github.com/Kinfxhk/vela/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-AGPL--3.0--or--later-blue)](LICENSE)
[![go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev)
![platforms](https://img.shields.io/badge/macOS%20%7C%20Linux-x64%20%7C%20arm64-lightgrey)

![Vela](docs/banner.png)

---

## 繁體中文

### 為何

你的影片分散在硬碟、NAS 或外接硬碟之中。你不希望將它們上傳到任何地方，也不希望為了尋找一段影片而安裝十個應用程式。

Vela 只做一件事：**指定你的資料夾，建立一個快速、美觀、可自訂的私人媒體庫。** 檔案留在原位，資料庫只記錄路徑與中繼資料，播放時直接串流原始檔案。

- 不托管、不提供、不索引任何內容；你使用自己的檔案。
- 預設只綁定 `127.0.0.1`，無遙測、無帳號。
- 單一 Go 二進位檔 + SQLite；ffmpeg 為可選項（封面、時長、字幕）。

### 功能

- **增量掃描** — 只處理新增或修改過的檔案，跳過尚未複製完成的檔案。
- **Range 串流** — 直接播放原始檔案，預設不轉碼；支援 206、seek、resume。
- **標籤、搜尋、篩選** — tag chips、即時搜尋、SQLite 索引。
- **一鍵整理** — 新增或移除標籤、續播位置、移至垃圾桶、原路徑還原。
- **批次操作** — 多選影片後一次加標籤或移到垃圾桶。
- **合集／儲存檢視** — 將目前的搜尋條件存成合集，日後一按重現。
- **播放清單** — 建立靜態清單、加入多選影片、調整次序。
- **A-B 循環** — 播放器內標記兩個時間點，自動循環該段。
- **收藏與隊列** — 星標收藏；播放器支援上一條／下一條、播完自動接續、0.75–2× 速度。
- **主題與自訂連結** — 主色、密度、圓角，以及側欄自訂外部連結。
- **主頁貨架** — 繼續觀看、最近加入、收藏三個貨架，開啟即見；不需要先搜尋。
- **版面預設** — 「簡潔」與「完整」一鍵切換；進階選項收在設定面板內。
- **桌面側欄與手機底部導覽** — 桌面用固定側欄，手機用底部五格導覽；排序由版面設定控制。
- **標籤管理介面** — 直接在介面改名、合併、刪除標籤。
- **資料庫工具** — 健康檢查（0 byte、缺時長、孤兒標籤）、重複檔一鍵清理、路徑重映射。
- **手掣回饋** — 可選的 `hapticsd` 守護程序，把播放聲音的低頻／高頻轉成手掣震動；播放器可調強弱。
- **本地字幕** — 使用 whisper.cpp 在你的電腦轉錄，模型可切換（tiny → large-v3）；可翻譯成英文，其他語言可連接本地 LLM。
- **本機 AI 標籤** — 單片、多選批次或整個資料夾抽取畫面幀，交由本機視覺模型提出建議；可在審核面板逐個接受或拒絕，不會自動寫入。
- **自訂介面** — sidebar 次序、密度、主題；`layout.json` 可匯入／匯出。
- **五種語言** — 繁體中文、简体中文、English、한국어、日本語；跟隨瀏覽器自動選擇。
- **輕量** — Linux 二進位檔約 10MB，閒置記憶體低。
- **私隱** — `X-Robots-Tag: noindex`，Private mode 一鍵關閉對外 listener。

### 主介面

![主介面](docs/screenshots/guide-main.png)

1. 側欄：導覽、連線狀態、語言、設定。
2. 頂欄：搜尋、掃描、儲存檢視、合集、播放清單、審核、工具。
3. 繼續觀看貨架。
4. 最近加入貨架。

- **左側側欄**：品牌、導覽項目、連線狀態、語言選擇、設定入口。桌面固定顯示，手機自動收起。
- **頂欄**：搜尋、掃描、儲存檢視、合集、播放清單、審核。
- **首頁**：繼續觀看、最近加入、收藏三個貨架；沒有內容時顯示安裝提示。
- **媒體庫**：網格卡片、標籤篩選、即時搜尋；可切換密度。
- **播放器**：上一條／下一條、自動接續、0.75–2× 速度、A-B 循環、字幕、AI 標籤建議、收藏、移至垃圾桶。
- **手機**：底部五格導覽（首頁、媒體庫、收藏、標籤、更多）。

### 設定頁面

![設定頁面](docs/screenshots/guide-settings.png)

1. 導覽次序：上下移動、顯示或隱藏。
2. 主題：主色、密度、圓角。
3. 自訂連結：名稱與網址。
4. 版面預設：簡潔／完整。
5. 標籤管理：改名、合併、刪除。

按側欄底部的「版面」開啟設定面板：

- **導覽次序**：上下移動、勾選顯示或隱藏。
- **版面預設**：「簡潔」只保留首頁、媒體庫、收藏；「完整」加入標籤。
- **主題**：主色、密度（緊湊／舒適／寬鬆）、圓角。
- **自訂連結**：輸入名稱與網址，加入側欄。
- **標籤管理**：改名、合併、刪除，並顯示每個標籤的使用次數。
- **儲存／還原**：儲存後寫入 `layout.json`；還原會重新載入已儲存版本。

其他設定位於 `config.json`（路徑見下方），包括 Whisper 模型、視覺模型、翻譯端點與連接埠。

### 操作教學

![批次操作](docs/screenshots/guide-actions.png)

1. 勾選卡片左上角方框。
2. 底部批次列：加標籤、AI、垃圾桶、清除。
3. AI：為選取項目產生標籤建議。
4. 審核：逐個接受或拒絕建議。

1. **首次安裝**：執行安裝指令，依序執行 `vela init`、`vela scan`、`vela serve`。
2. **瀏覽**：首頁貨架直接開啟；按「媒體庫」進入完整網格；搜尋框即時過濾。
3. **整理**：勾選卡片左上角方框可多選，底部出現批次列，可加標籤或移至垃圾桶。
4. **AI 標籤**：先啟動本機視覺模型（例如 llama.cpp server）。單片可在播放器按「AI」；批次可勾選多張卡片後按底部「AI」；整個資料夾可在「工具」輸入路徑後按套用。完成後在頂欄「審核」逐個按 ✓／×，或一次接受 ≥ 90% 的建議。
5. **字幕**：在播放器按「字幕」生成；模型可用 `vela subs <id> --model small` 切換；播放器可即時加減字幕偏移。
6. **播放**：`←`／`→` 前後 5 秒、空白鍵播放暫停、`F` 全螢幕、`N` 下一條。
7. **備份**：`layout.json` 可從設定面板或 API 匯出；資料庫可用 SQLite 的 `VACUUM INTO` 備份。
8. **手掣回饋（可選）**：在 Linux 以 USB 接上手掣，執行 `hapticsd --driver auto`；播放器按 ◎ 開啟，右側滑桿調強弱。`--driver mock` 可在沒有手掣時測試。

### 快速開始

```sh
curl -fsSL https://raw.githubusercontent.com/Kinfxhk/vela/main/install.sh | sh
vela init      # 選擇資料夾與連接埠
vela scan      # 建立索引
vela serve     # 開啟 http://127.0.0.1:8770
```

Docker：

```sh
docker compose up -d
```

如需自行建置：

```sh
git clone https://github.com/Kinfxhk/vela && cd vela
go build -o vela . && ./vela init && ./vela scan && ./vela serve
```

需要 macOS 12+ / Linux x64 / arm64；Go 1.23+ 僅在建置時需要。`vela doctor` 會檢查資料夾、ffmpeg、資料庫與連接埠。

### 字幕（本地 Whisper）

```sh
vela subs <media-id> --model base --lang auto
vela subs <media-id> --model small --lang ja --translate en
```

- 模型：`tiny` / `base` / `small` / `medium` / `large-v3`，或直接提供 `.bin` 路徑。預設 `base`，存放於 `~/.vela/models/ggml-<model>.bin`。
- 轉錄輸出 SRT，存入資料庫；播放器自動掛載 `<track>`，可選擇語言或關閉。
- 英文翻譯使用 whisper 內建 `--translate`；繁體中文／简体中文／韓文／日文翻譯連接本地 OpenAI-compatible endpoint（預設 Ollama `127.0.0.1:11434`，模型可更改）。

### 自訂介面

`layout.json` 控制 sidebar 次序與顯示；可直接在介面上下移動，亦可使用 `GET/PUT /api/layout`。版面可以備份、分享、搬遷。

### 語言

介面內建 English、繁體中文、简体中文、한국어、日本語；可使用 `?lang=ja` 或跟隨瀏覽器語言自動選擇，選擇儲存於 `localStorage`。

### 文件

- [Roadmap](docs/ROADMAP.md)
- [標籤設計](docs/TAGS.md)
- [介面檢討](docs/UI-REVIEW.md)
- API：見下方 English › API

### 授權

AGPL-3.0-or-later。名稱與 logo 不包含在程式碼授權內；fork 不得使用。

---

## English

### Why

Your videos are already on disks, a NAS, or an external drive. You do not want to upload them anywhere, and you do not want ten apps to find one clip.

Vela does one thing: **point it at your folders and get a fast, good-looking, customisable private library.** Files stay where they are; the database stores paths and metadata; playback streams the original file.

- It does not host, supply, or index content. You bring your own files.
- Binds `127.0.0.1` by default. No telemetry, no accounts.
- One Go binary + SQLite. ffmpeg is optional (covers, duration, subtitles).

### Features

- **Incremental scan** — only new/changed files; skips files still being copied.
- **Range streaming** — original file, no transcode by default; 206, seek, resume.
- **Tags, search, filters** — tag chips, instant search, SQLite index.
- **One-click care** — add/remove tags, resume position, move to trash, restore in place.
- **Bulk actions** — select many items, tag them or move them to trash at once.
- **Collections** — save the current search as a named view and restore it later.
- **Playlists** — create static lists, add selected items, reorder them.
- **A-B loop** — mark two points in the player and loop that section.
- **Favourites and queue** — star items; previous/next, autoplay next, 0.75–2× speed.
- **Theme and links** — accent colour, density, radius, plus custom external links in the sidebar.
- **Home shelves** — continue watching, recently added, and favourites on first open; no search required.
- **Layout presets** — one click between Simple and Full; advanced options stay in the settings panel.
- **Desktop rail and mobile bottom nav** — a fixed rail on desktop, five-slot bottom nav on phones; ordering comes from the layout file.
- **Tag manager UI** — rename, merge, and delete tags without leaving the app.
- **Library tools** — health check (zero-byte, missing duration, orphan tags), one-click duplicate cleanup, and path remapping.
- **Controller feedback** — an optional `hapticsd` daemon maps low/high audio bands to rumble; intensity is adjustable in the player.
- **Local subtitles** — whisper.cpp on your machine, switchable models (tiny → large-v3); English translation built in, other languages via a local LLM endpoint.
- **Local AI tags** — sample frames for one item, a selection, or a whole folder, ask a local vision model, then accept or reject each suggestion in the review panel; nothing is written silently.
- **Custom UI** — sidebar order, density, theme; `layout.json` imports/exports.
- **5 locales** — English, 繁體中文, 简体中文, 한국어, 日本語; follows the browser.
- **Small** — ~10MB Linux binary, low idle memory.
- **Private** — `X-Robots-Tag: noindex`; private mode stops non-loopback listeners.

### Main interface

![Main interface](docs/screenshots/guide-main.png)

1. Rail: navigation, status, language, settings.
2. Top bar: search, scan, save view, collections, playlists, review, tools.
3. Continue-watching shelf.
4. Recently-added shelf.

- **Left rail**: brand, navigation, connection status, language, settings. Fixed on desktop, hidden on phones.
- **Top bar**: search, scan, save view, collections, playlists, review.
- **Home**: continue watching, recently added, favourites shelves; an install hint when empty.
- **Library**: card grid, tag filters, instant search, density switch.
- **Player**: previous/next, autoplay next, 0.75–2× speed, A-B loop, subtitles, AI tag suggestions, favourite, trash.
- **Mobile**: five-slot bottom navigation (Home, Library, Favourites, Tags, More).

### Settings

![Settings](docs/screenshots/guide-settings.png)

1. Navigation order: move, show or hide.
2. Theme: accent, density, radius.
3. Custom link: name and URL.
4. Presets: Simple / Full.
5. Tag manager: rename, merge, delete.

Open the Layout panel from the bottom of the rail:

- **Navigation order**: move up/down, show or hide items.
- **Presets**: Simple keeps Home, Library, Favourites; Full adds Tags.
- **Theme**: accent colour, density (compact/comfortable/roomy), corner radius.
- **Custom links**: name + URL, added to the rail.
- **Tag manager**: rename, merge, delete, with usage counts.
- **Save / reset**: writes `layout.json`; reset reloads the saved layout.

Other settings live in `config.json` (path below): Whisper model, vision model, translation endpoint, and address.

### Walkthrough

![Batch actions](docs/screenshots/guide-actions.png)

1. Tick the box on a card.
2. Bulk bar: tag, AI, trash, clear.
3. AI: generate tag suggestions for the selection.
4. Review: accept or reject each suggestion.

1. **First run**: install, then `vela init`, `vela scan`, `vela serve`.
2. **Browse**: the home shelves open first; Library shows the full grid; the search box filters as you type.
3. **Organise**: tick the box on a card to multi-select; the bulk bar adds tags or moves items to trash.
4. **AI tags**: start a local vision server (for example llama.cpp). For one item press AI in the player; for a batch tick cards and press AI in the bulk bar; for a folder use Tools → AI tag a folder. Then open Review in the top bar and accept ✓ or reject ×, or accept everything ≥ 90% at once.
5. **Subtitles**: press Subtitles in the player; switch models with `vela subs <id> --model small`; adjust the offset live in the player.
6. **Playback**: `←`/`→` seek 5s, space toggles, `F` fullscreen, `N` next.
7. **Backup**: export `layout.json` from settings or the API; back up SQLite with `VACUUM INTO`.
8. **Controller feedback (optional)**: on Linux with a USB controller, run `hapticsd --driver auto`; press ◎ in the player and adjust the slider. `--driver mock` tests without hardware.

### Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/Kinfxhk/vela/main/install.sh | sh
vela init
vela scan
vela serve     # http://127.0.0.1:8770
```

Docker:

```sh
docker compose up -d
```

Build from source:

```sh
git clone https://github.com/Kinfxhk/vela && cd vela
go build -o vela . && ./vela init && ./vela scan && ./vela serve
```

macOS 12+ / Linux x64 / arm64. Go 1.23+ is only needed to build. `vela doctor` checks folders, ffmpeg, database, and port.

### Subtitles (local Whisper)

```sh
vela subs <media-id> --model base --lang auto
vela subs <media-id> --model small --lang ja --translate en
```

- Models: `tiny` / `base` / `small` / `medium` / `large-v3`, or a `.bin` path. Default `base` at `~/.vela/models/ggml-<model>.bin`.
- Writes SRT, stores it in the database; the player attaches a `<track>` automatically with a language selector.
- English translation uses whisper `--translate`; zh-Hant / zh-Hans / ko / ja translation uses a local OpenAI-compatible endpoint (default Ollama `127.0.0.1:11434`, model configurable).

### Custom UI

`layout.json` controls sidebar order and visibility. Reorder in the UI or `GET/PUT /api/layout`. Layouts are portable.

### API

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | server + scan state |
| GET | `/api/stats` | files, bytes, duration, tags |
| GET | `/api/media?q=&tag=&sort=&limit=&offset=` | list/search |
| GET | `/api/media/{id}` | detail + resume position |
| GET | `/api/media/{id}/stream` | Range streaming |
| GET | `/api/media/{id}/cover` | JPEG thumbnail |
| GET | `/api/media/{id}/subtitles` | list tracks |
| GET | `/api/media/{id}/subtitles/{lang}?kind=` | WebVTT |
| POST | `/api/media/{id}/subtitles` | generate (local) |
| POST | `/api/media/{id}/tags` | `{"add":[],"remove":[]}` |
| POST | `/api/media/{id}/progress` | `{"position_ms":1234}` |
| POST | `/api/media/{id}/trash` | move to `.vela-trash/` |
| POST | `/api/media/{id}/restore` | move back |
| POST | `/api/scan` | `{"full":false}` |
| GET/PUT | `/api/layout` | UI layout JSON |
| GET/POST | `/api/collections` | saved searches |
| DELETE | `/api/collections/{id}` | delete a saved search |
| GET | `/api/duplicates` | same size + duration groups |
| GET | `/api/library/health` | zero-byte, missing duration, orphan tags |
| POST | `/api/remap` | rewrite a path prefix after a move |
| POST | `/api/bulk` | tag / untag / trash / restore many |
| POST | `/api/tags/rename` | rename (merges on conflict) |
| POST | `/api/tags/merge` | merge two tags |
| POST | `/api/tags/alias` | alias → canonical tag |
| DELETE | `/api/tags/{name}` | delete a tag |

### FAQ

**Is Vela a downloader?** No. It indexes folders you already own. A generic URL ingest is on the roadmap; there are no site-specific extractors in core.

**Does it upload anything?** No. No telemetry, no accounts, no cloud calls.

**Why Go?** The bottleneck is disk, SQLite, and ffmpeg, not HTTP throughput. Go gives one static binary per platform and a small memory footprint.

**Can I use it on a NAS?** Yes. Run the binary or the Docker image on the NAS and mount media read-only. Keep the database on local disk.

### Docs

- [Roadmap](docs/ROADMAP.md)
- [Tag design](docs/TAGS.md)
- [UI review](docs/UI-REVIEW.md)

### License

AGPL-3.0-or-later. See [LICENSE](LICENSE). The name and logo are not covered by the code license.
