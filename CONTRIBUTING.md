# Contributing to annoybots

Thanks for your interest! annoybots is a framework for chat "nuisance" bots —
one Go binary, many config-defined personalities. Bug reports, new transports,
and quality-of-life fixes are all welcome.

## Getting started

```sh
make build    # go build -trimpath -o bin/annoybot ./cmd/annoybot
make test     # go test ./...
make lint     # golangci-lint run
```

To run a bot locally, copy an example config and point the binary at it (export
the `*_env` secrets it references first):

```sh
make run CONFIG=configs/echo.yaml
```

See the [README](README.md) for the config format, the "Add a bot" guide, and
how the botnet/skits work, and [`docs/`](docs/) for the command, IdleRPG, plugin,
accounts, and federation references.

## Pull requests

1. **Branch off `main`** using `<type>/<slug>` (e.g. `feat/discord-threads`,
   `fix/markov-panic`).
2. **Keep PRs focused** — one logical change per PR.
3. **Add tests** for behavior changes. Every `internal/` package has a
   `_test.go`; `miniredis` stands in for the botnet bus, so transports and the
   engine are testable without live networks.
4. **Make CI pass.** Every PR runs `go vet`, `go test -race ./...`,
   `golangci-lint`, and a Docker image build — all four must be green.

## Commits

This repo uses [Conventional Commits](https://www.conventionalcommits.org/) — the
type drives the changelog and the next release version via `release-please`:

```
feat(discord): add thread support
fix(markov): guard against empty-state reload
```

Common types: `feat`, `fix`, `perf`, `refactor`, `ci`, `docs`, `chore`.

**Sign off every commit** (Developer Certificate of Origin):

```sh
git commit -s -m "feat(scope): summary"
```

## Secrets

Never commit real tokens or passwords. Secrets are referenced indirectly — each
`*_env` field in a config names an environment variable populated from a
Kubernetes Secret. See [`deploy/k8s/overlays/<bot>/secret.example.yaml`](deploy/k8s)
for the template. Found a vulnerability? See [SECURITY.md](SECURITY.md) — please
don't open a public issue for it.

## License

By contributing, you agree your contributions are licensed under the project's
[MIT License](LICENSE).
