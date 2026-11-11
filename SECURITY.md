# Security policy

TrueDeploy holds SSH credentials, database passwords and Git tokens, so security reports matter a lot.

## Reporting a vulnerability

Please **don't open a public issue**. Use GitHub's private reporting instead:
[Report a vulnerability](https://github.com/HamzaBaghdadi/TrueDeploy/security/advisories/new).

Include what you found, how to reproduce it, and what an attacker could do with it. You'll get an answer within a few days, and credit in the release notes if you'd like.

## Supported versions

Security fixes go into the latest release. The app updates itself, so please stay on the newest version.

## How TrueDeploy protects your data

- **Secrets stay on your computer, encrypted.** SSH passwords and keys, environment variables, database passwords, and Git and storage tokens are encrypted with AES-256-GCM. The key lives in your OS keychain (Windows Credential Manager, macOS Keychain, or the Secret Service on Linux).
- **Host keys are pinned** on first connection. A changed key stops the connection.
- **Nothing extra runs on your servers.** There is no TrueDeploy agent, dashboard or open port: only your Docker containers and the Caddy proxy.
- **Auto-deploy keys are locked down.** The CI key can run only one deploy command on the server (a forced command in `authorized_keys`), and your app's secrets never leave the server.
- **Exports are encrypted** with your passphrase (Argon2id + AES-256-GCM).
- **Updates are verified.** They're downloaded from GitHub Releases over HTTPS and checked against the release's SHA-256 checksums before they're installed.
