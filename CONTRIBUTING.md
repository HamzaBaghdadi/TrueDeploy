# Contributing to TrueDeploy

Thanks for helping! Bug reports, ideas, docs and code are all welcome.

## Before you start

- **Bugs and ideas:** open an [issue](https://github.com/HamzaBaghdadi/TrueDeploy/issues/new/choose) first, so we can agree on the approach before you spend time on code.
- **Security problems:** please don't open a public issue. See [SECURITY.md](SECURITY.md).
- Small fixes (typos, obvious bugs) can go straight to a pull request.

## Set up a development environment

You need:

- [Go](https://go.dev/dl/) 1.26 or newer
- [Node.js](https://nodejs.org/) 24 or newer and [pnpm](https://pnpm.io/installation)
- The Wails CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`
- On Linux: `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (Debian/Ubuntu package names)

Then:

```bash
git clone https://github.com/HamzaBaghdadi/TrueDeploy.git
cd TrueDeploy
cd frontend && pnpm install && cd ..
wails3 dev
```

`wails3 dev` opens the app with hot reload: the UI reloads on save, and Go changes rebuild the app.

## How the code is organised

```
main.go              App entry: registers the Go services and embeds the UI
services/            Go services the UI calls (generated as TypeScript functions)
internal/            The real work, one package per area:
  sshx/                SSH connections, host-key pinning, commands, terminals
  setup/               Server setup plans (Docker, firewall, updates, no-sudo mode)
  deploy/              Deploy scripts (Compose, Dockerfile, health checks, rollbacks)
  presets/             Framework presets that generate Dockerfiles
  proxy/               Caddy config: domains, HTTPS, paths, redirects, passwords
  databases/           Managed databases and their backups
  gitprov/             GitHub, GitLab, Bitbucket and Gitea APIs
  autodeploy/          Deploy-on-push hooks and CI pipelines
  offsite/, s3/        Offsite backup copies to S3-compatible storage
  updates/             Release checks and self-update
  store/               Local data, with secrets encrypted (key in the OS keychain)
frontend/            Next.js (static export) + Tailwind + shadcn/ui
build/               Icons, installers and per-platform build tasks
```

The UI never talks to servers directly: everything goes through a Go method in `services/`. After changing a service's methods, regenerate the TypeScript bindings:

```bash
wails3 generate bindings -clean=true -ts -i
go run ./build/tools/fixbindings frontend/bindings
```

Remote work happens through **bash scripts sent over SSH**. They're built in Go (see `internal/deploy`, `internal/setup` and others), and their tests run them against fake `docker`, `crontab` and friends, so most behaviour can be tested without a real server.

## Checks before a pull request

```bash
go vet ./...
go test ./...
cd frontend && pnpm lint && pnpm build && pnpm exec tsc --noEmit
```

On Windows, the script tests use Git Bash; they're skipped when no suitable bash is found. The same checks run on every pull request.

## Style

- **Go:** `gofmt`, small packages, and errors a person can act on ("Can't reach Docker as deploy. Add this user to the docker group…" rather than "exit status 1").
- **UI:** shadcn/ui components and lucide icons only; see [frontend/UI.md](frontend/UI.md). No native browser dialogs.
- **Words:** plain, short sentences. Say what happened and what to do next.
- **Servers:** never assume `sudo`. Scripts should work for a normal user where possible and say clearly when they can't.

## Releases

Maintainers release by bumping the version in `services/app.go`, `build/config.yml` and `build/windows/info.json`, then pushing a tag like `v0.2.0`. GitHub Actions builds every platform and publishes the release; running apps offer the update automatically.

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
