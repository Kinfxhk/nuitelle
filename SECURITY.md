# Security

Report vulnerabilities privately to the maintainer email listed on the GitHub profile. Please include:

- Vela version (`vela version`) and OS
- a minimal reproduction
- impact assessment

Do not open a public issue before a fix is available.

Vela binds `127.0.0.1` by default. If you expose it on a LAN or the internet, put it behind a reverse proxy with TLS and authentication; the built-in server has no auth layer.

Path handling: streaming and trash operations are restricted to configured roots after symlink resolution. Report any bypass with exact steps.
