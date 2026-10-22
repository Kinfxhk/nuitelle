# Tag design — what to tag, and how

Tags are the part users feel every day. A bad scheme becomes 900 near-duplicates; a good one stays under ~200 active tags for a 100k-file library.

## 1. Rules

1. **Facet first, tag second.** Every tag belongs to one facet (setting, look, shot…). The UI groups by facet; the DB stores a canonical slug.
2. **One canonical slug, many labels.** `closeup` is the key; `特寫 / 特写 / クローズアップ / 클로즈업` are display labels. Never store translated strings as separate tags.
3. **Singular, lowercase, hyphenated.** `closeup`, `soft-light`, `two-people`. No spaces, no emoji, no punctuation.
4. **3–8 tags per item.** If everything is tagged, nothing is findable. Facets are prompts, not a checklist.
5. **Controlled core + freeform extras.** The seed pack is curated; users may add their own under `user` facet. AI suggestions never invent new tags by default.
6. **Aliases absorb synonyms.** `cos`, `cosplay`, `コスプレ` → one tag with aliases. Merge tool: pick winner, rewrite memberships.
7. **No personal data in tags.** No names, addresses, or site handles as tags; those belong to performer/source fields.
8. **AI proposes, human confirms.** Confidence < 0.65 → review queue; 0.65–0.85 → suggested; > 0.85 → still one click to accept. Never silently write tags.

## 2. Facets (seed pack)

| Facet | Slug | EN | 繁中 | 简中 | 日本語 | 한국어 |
|---|---|---|---|---|---|---|
| setting | indoor | Indoor | 室內 | 室内 | 屋内 | 실내 |
| setting | outdoor | Outdoor | 戶外 | 户外 | 屋外 | 야외 |
| setting | studio | Studio | 攝影棚 | 摄影棚 | スタジオ | 스튜디오 |
| setting | home | Home | 居家 | 居家 | 自宅 | 자택 |
| setting | hotel | Hotel | 酒店 | 酒店 | ホテル | 호텔 |
| setting | nature | Nature | 自然 | 自然 | 自然 | 자연 |
| people | solo | Solo | 單人 | 单人 | ソロ | 솔로 |
| people | duo | Duo | 雙人 | 双人 | 2人 | 2인 |
| people | group | Group | 多人 | 多人 | グループ | 그룹 |
| look | casual | Casual | 便服 | 便服 | 私服 | 사복 |
| look | uniform | Uniform | 制服 | 制服 | 制服 | 제복 |
| look | costume | Costume | 角色扮演 | 角色扮演 | コスチューム | 코스튬 |
| look | lingerie | Lingerie | 內衣 | 内衣 | ランジェリー | 란제리 |
| look | swimwear | Swimwear | 泳裝 | 泳装 | 水着 | 수영복 |
| look | formal | Formal | 正裝 | 正装 | フォーマル | 정장 |
| shot | closeup | Close-up | 特寫 | 特写 | クローズアップ | 클로즈업 |
| shot | wide | Wide | 全景 | 全景 | 全景 | 와이드 |
| shot | pov | POV | 主觀 | 主观 | POV | POV |
| shot | vertical | Vertical | 直式 | 竖屏 | 縦動画 | 세로 |
| action | dance | Dance | 跳舞 | 跳舞 | ダンス | 댄스 |
| action | interview | Interview | 訪談 | 访谈 | インタビュー | 인터뷰 |
| action | walk | Walk | 步行 | 步行 | 散歩 | 산책 |
| action | exercise | Exercise | 運動 | 运动 | 運動 | 운동 |
| mood | soft | Soft | 柔和 | 柔和 | ソフト | 부드러움 |
| mood | energetic | Energetic | 活力 | 活力 | 元気 | 활기 |
| mood | dark | Dark | 暗調 | 暗调 | ダーク | 어두움 |
| mood | playful | Playful | 俏皮 | 俏皮 | 遊び心 | 장난스러움 |
| quality | 4k | 4K | 4K | 4K | 4K | 4K |
| quality | hdr | HDR | HDR | HDR | HDR | HDR |
| quality | restored | Restored | 修復 | 修复 | 復元 | 복원 |
| quality | subtitled | Subtitled | 有字幕 | 有字幕 | 字幕付き | 자막 |
| status | favorite | Favorite | 最愛 | 最爱 | お気に入り | 즐겨찾기 |
| status | archived | Archived | 已歸檔 | 已归档 | アーカイブ | 보관됨 |
| status | unwatched | Unwatched | 未看 | 未看 | 未視聴 | 미시청 |
| source | imported | Imported | 匯入 | 导入 | 取り込み | 가져옴 |
| source | recorded | Recorded | 錄影 | 录制 | 録画 | 녹화 |
| source | downloaded | Downloaded | 下載 | 下载 | ダウンロード | 다운로드 |

37 tags, 9 facets — enough for a 100k library. Community packs may add facets (e.g. `series`, `camera`, `audio`) without touching core.

## 3. DB shape

```sql
CREATE TABLE tag (
  id INTEGER PRIMARY KEY,
  slug TEXT UNIQUE NOT NULL,     -- canonical key, ASCII
  facet TEXT NOT NULL,           -- setting | people | look | ...
  label_en TEXT NOT NULL,
  color TEXT DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE tag_label (
  tag_id INTEGER NOT NULL,
  lang TEXT NOT NULL,            -- en | zh-Hant | zh-Hans | ja | ko
  label TEXT NOT NULL,
  PRIMARY KEY (tag_id, lang)
);
CREATE TABLE tag_alias (
  alias TEXT PRIMARY KEY,
  tag_id INTEGER NOT NULL
);
CREATE TABLE media_tag (
  media_id TEXT NOT NULL,
  tag_id INTEGER NOT NULL,
  source TEXT NOT NULL DEFAULT 'user',  -- user | ai | import
  confidence REAL,
  PRIMARY KEY (media_id, tag_id)
);
```

The current 0.1.x schema (`tags.name`) migrates by: lowercase → trim → alias lookup → create slug.

## 4. AI tagging workflow

1. Pick a **folder** or a **single video**.
2. Sample 3 frames per video (10%, 50%, 90%) at 448 px; build a 4×2 contact sheet for context.
3. Local VLM (Qwen3-VL 2B/4B via llama.cpp/MLX) returns JSON: `scene`, `tags[{tag,confidence}]`.
4. Map returned labels to canonical slugs via alias table; unmapped labels go to `user` facet as suggestions.
5. Write `media_tag` rows with `source='ai'`, `confidence`.
6. UI review queue: accept / reject / accept-all-above-0.85; accepted rows flip to `source='user'`.
7. Cache by `(media_id, mtime, model)` so re-runs are free.

Measured on this Mac: 2B Metal ≈ 0.54 s/frame, 3 frames ≈ 1.6 s/video → **1,000 clips ≈ 27 min**; CPU ≈ 2.8 h. Use a background job and resume by media id.

## 5. What not to tag

- Resolution/bitrate (already metadata, filter on the column).
- File path, extension, source site (source is a field).
- Transient states (“new”, “downloaded today”) — use smart collections.
- Anything only one item will ever have.

## 6. Seed JSON

`docs/tags-seed.json` contains the 37 tags with five labels each; import is idempotent by slug.
