# Contributing

Thanks for helping. Vela is a **content-neutral, local-first** media library. Keep that posture in every change.

## Ground rules

- **No site-specific extractors or scrapers.** Core accepts a user-supplied URL; it never ships code that targets a particular website.
- **No DRM circumvention, paywall bypass, or credential collection.**
- **No bundled media, sample content, or screenshots of third-party material.**
- **No telemetry, cloud calls, or accounts.** The server binds loopback by default.
- **Plugin UI is JSON only.** Do not add script injection; the component allowlist is the security boundary.
- Keep user-facing text neutral: `library`, `ingest`, `feedback`, `restore`.

## Development

```sh
go test ./...
go build -o bin/vela .
./bin/vela init && ./bin/vela scan && ./bin/vela serve
```

- Go 1.23+ (CI uses 1.27).
- SQLite is pure Go (`modernc.org/sqlite`); CGO is not required.
- ffmpeg/ffprobe are optional at runtime.

## Pull requests

1. One concern per PR.
2. Add or update a test for behaviour changes.
3. Run `go test ./...` and `go vet ./...`.
4. Describe the user-visible effect and any migration.
5. Do not commit databases, generated covers, or media.

## Reporting

Open an issue with the OS, version (`vela version`), and the exact command. Do not attach private media or paths you do not want public.

## Security

Email the maintainer listed in `SECURITY.md` for vulnerabilities. Do not open a public issue first.
