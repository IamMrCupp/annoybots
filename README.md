# annoybots

Modern, containerized rebirth of two classic [BMotion](http://bmotion.sourceforge.net/)-style
IRC nuisance bots — **Arywen** and **Kurkutu** — for Kubernetes.

One small Go binary holds many simultaneous chat connections at once (real IRC
networks, a private InspIRCd test net, and Twitch) and routes every channel
through a shared "annoyance engine." Each bot is the same binary with a
different personality file.

## What it does

- **Keyword/regex triggers** → randomized, templated responses (`{nick}`, `{me}`, `{chan}`, capture groups).
- **Ambient interjections** — random unprompted lines, with per-channel cooldowns.
- **Quote packs** — drop-in `.txt` files (Rick & Morty, South Park, classic bot sass) surfaced randomly and via `!quote [pack]`.
- **A learning "brain"** — an order-N Markov chain that learns from channel chatter and babbles it back, mangled. It persists to disk so learning survives restarts (just like the BMotion babble everyone remembers, minus the abandoned TCL stack).
- **Multi-network in one process** — IRC + Twitch share the same wire protocol; Twitch just needs CAP negotiation, an `oauth:` token, and tighter rate limits, all handled automatically.

## Layout

```
cmd/annoybot         entrypoint
internal/engine      annoyance engine (triggers, interjections, quotes, commands)
internal/markov      persistent Markov "brain"
internal/irc         multi-network client (ergochat/irc-go) + per-network rate limiting
internal/ratelimit   token-bucket limiter (Twitch-aware)
internal/cooldown    per-channel cooldown tracking
internal/config      YAML config loading, validation, quote-pack loading
internal/health      /healthz + /readyz for Kubernetes probes
configs/             arywen.yaml, kurkutu.yaml (personality + networks)
data/quotes/         starter quote packs
deploy/k8s/          kustomize base + per-bot overlays
```

## Run locally

```sh
# Export the secrets your config references first, e.g.:
export ARYWEN_LIBERA_SASL=...           # NickServ/SASL password
export ARYWEN_TWITCH_TOKEN=oauth:...    # Twitch chat token

make run-arywen
```

Quote-pack files resolve relative to `ANNOYBOT_QUOTES_DIR` (the Makefile points
it at `data/quotes`).

## Configuration

Everything behavioral lives in `configs/<bot>.yaml`: networks, triggers,
interjection/quote rates and cooldowns, and Markov settings. **Secrets are never
stored in the config** — each `*_env` field names an environment variable
(populated from a Kubernetes Secret) that holds the actual token or password.

### Twitch

Set `kind: twitch` on a network and point `password_env` at an env var holding a
chat oauth token. Server, TLS, CAPs, and conservative rate limits default
automatically. Note Twitch does not reliably broadcast joins/parts or mode
changes, so user/op tracking there is intentionally not relied upon.

## Deploy to Kubernetes

```sh
cd deploy/k8s/overlays/arywen
cp secret.example.env secret.env      # fill in real tokens; secret.env is gitignored

# The bot config lives in ../../../../configs, so disable the load restrictor:
kubectl kustomize --load-restrictor LoadRestrictionsNone . | kubectl apply -f -
```

Each bot deploys as a single-replica StatefulSet with its own PVC for the Markov
brain. Repeat for `overlays/kurkutu`.

## Adding quotes

Drop a `whatever.txt` file in `data/quotes/` (one quote per line, `#` comments
allowed), then reference it in a bot's `personality.quotes.packs`. Rebuild the
image (packs are baked in at `/quotes`) or mount them via a ConfigMap.

## Develop

```sh
make test     # go test ./...
make lint     # golangci-lint
make docker   # build the image
```
