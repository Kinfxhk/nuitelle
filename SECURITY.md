# Security

Report vulnerabilities privately to the maintainer email listed on the GitHub profile. Please include:

- Vela version (`vela version`) and OS
- a minimal reproduction
- impact assessment

Do not open a public issue before a fix is available.

Vela binds `127.0.0.1` by default. If you expose it on a LAN or the internet, put it behind a reverse proxy with TLS and authentication; the built-in server has no auth layer.

Path handling: streaming and trash operations are restricted to configured roots after symlink resolution; containment is checked with `filepath.Rel`, not a string prefix.

Request hardening: the server rejects non-loopback `Host` headers (DNS rebinding), cross-site `Origin` headers, and `text/plain` writes (CSRF). Layout JSON is schema-checked and links must be `http(s)`. Unknown `/api/*` paths return 404 instead of the SPA.

`hapticsd` accepts loopback connections only and rejects non-loopback `Origin` headers.

URL ingest resolves the host and rejects loopback, private, link-local, and unspecified addresses unless `ingest_allow_private` is enabled.

Login is rate-limited: five failures from one address trigger a five-minute lockout. Plugin VMs also cap the JS call stack and are interrupted after two seconds.

Report any bypass with exact steps.
