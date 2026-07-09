# Rooter

Rooter is a small OpenAI-compatible model router. It exposes one `/v1` API surface, lets you choose which upstream models appear in `/v1/models`, and rewrites requests to the configured provider and upstream model.

It builds to a single Go binary and stores settings in a JSON file outside the binary, so upgrades keep the configured providers, model filters, ordering, and API keys.

## Run

```sh
go run . -addr :8080
```

Then open `http://localhost:8080`.

By default settings are stored at your OS config path, usually:

```text
~/Library/Application Support/rooter/config.json
```

Rooter creates a random admin token in this file on first run. Use that token in
the admin UI, or set `ROOTER_ADMIN_TOKEN` to override the saved token.

Override it when deploying:

```sh
ROOTER_CONFIG=/var/lib/rooter/config.json ./rooter -addr :8080
```

## Build

```sh
go build -o rooter .
```

The GitHub Actions workflow builds `rooter-linux-amd64` on every push. When the
push is a tag, it also uploads that binary to a GitHub Release.

## Updates

Release-built binaries know their GitHub repository and can update themselves
from the latest release asset:

```sh
./rooter -update
```

To check before serving and exit after installing an update:

```sh
./rooter -auto-update -addr :8080
```

For local builds, pass the update repository explicitly:

```sh
ROOTER_UPDATE_REPO=owner/repo ./rooter -update
```

Updates require a matching SHA-256 sidecar asset and Ed25519 signature named
after the executable, for example `rooter-linux-amd64.sha256` and
`rooter-linux-amd64.sha256.sig`. Release builds embed
`ROOTER_UPDATE_PUBLIC_KEY` from GitHub Actions secrets as a base64 or hex
encoded raw Ed25519 public key, and tagged release builds sign with the
PEM-encoded Ed25519 private key in the `ROOTER_UPDATE_SIGNING_KEY` secret.

## Providers

Provider base URLs should point at the OpenAI-compatible API root:

- OpenAI-compatible: `https://api.example.com/v1`
- Local Ollama: `http://localhost:11434/api`
- Ollama Cloud: `https://ollama.com/api`

Ollama providers use Ollama's native `/api` surface behind the scenes and Rooter translates chat completions, completions, embeddings, and streams back into OpenAI-compatible responses.

## Admin model activation

The admin UI supports two ways to add models:

- Discover models from a provider and enable the subset you want to expose.
- Activate model names manually by pasting one upstream model name per line.

Manual activation is useful for cloud models that do not show up in a list endpoint. For Ollama providers, you can optionally call `/api/pull` before saving the model row. For direct Ollama Cloud usage, Rooter still sends inference requests to `https://ollama.com/api` with the provider API key configured in the admin UI.

Rooter does not pass client API keys through. Clients authenticate to Rooter with one of the public API keys configured in the admin UI. Rooter sends each provider its own configured API key.

## Model chains

Model chains are named ordered lists of provider/model steps. In the admin UI, create a chain such as `Coding`, choose each step from configured provider/upstream model dropdowns, then assign that chain to a visible public model. Chain step dropdowns include configured models even when they are not enabled in `/v1/models`.

Clients still see and request one regular public model name. For a chain-backed model, Rooter tries each configured chain step in order if the upstream returns any `4xx` response. Older configs with inline per-model fallback steps continue to work.

`429` responses also put that provider/model step on cooldown. Rooter uses `Retry-After` when present, and otherwise waits 30 minutes before trying that step again. Non-`4xx` responses, including upstream `5xx` errors, are returned without falling through the chain.

## Endpoints

For OpenAI-compatible providers, Rooter proxies `POST /v1/*` requests that contain a top-level JSON `model` field.

For Ollama and Ollama Cloud providers, Rooter translates these endpoints through Ollama's native API:

- `GET /v1/models`
- `GET /v1/models/{model}`
- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/embeddings`

Requests must include:

```http
Authorization: Bearer <rooter-public-api-key>
```

## Admin security

Rooter requires an admin token for the admin API. A token is generated in the
settings file when one is missing. Set `ROOTER_ADMIN_TOKEN` to override the saved
admin token.

The admin API accepts either:

```http
Authorization: Bearer <admin-token>
```

or:

```http
X-Rooter-Admin-Token: <admin-token>
```
