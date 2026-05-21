# HalloWa Backend (Go)

Drop-in replacement for the Node.js Baileys backend. Talks to the same Supabase project + frontend, same `devices`, `backend_servers`, broadcast tables. Uses [whatsmeow](https://github.com/tulir/whatsmeow) instead of Baileys.

## Why Go

- 10-30x lower memory per device
- Native concurrency via goroutines (no worker threads)
- Single static binary, no `node_modules`
- Strong typing, fewer runtime surprises

## Architecture

```
cmd/hallowa-be/main.go              entry point
internal/
  config/                           env loader
  logger/                           structured logs
  supabase/                         REST client (devices, backend_servers, broadcasts)
  server/                           server identity + registration
  store/                            whatsmeow SQLite session store
  whatsapp/                         per-device WA client manager
  httpserver/                       /health, /send-message, /api/*
  broadcast/                        scheduled + queued broadcasts (phase 2)
```

## Quickstart

```
cp .env.example .env
# fill SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY, SERVER_NAME
go build -o hallowa-be ./cmd/hallowa-be
./hallowa-be
```

## Compatibility with the Node backend

This service registers itself in `backend_servers` (auto-creates row by `server_url`), polls `devices` where `assigned_server_id = self.id`, generates QR or pairing code via whatsmeow, and pushes the result to `devices.qr_code` / `devices.pairing_code` so the existing Supabase realtime channel feeds the FE without changes.

## Phase plan

- **Phase 1 (this commit)**: device connect, QR/pairing, send-message.
- **Phase 2**: broadcast queue (Asynq), scheduled broadcasts.
- **Phase 3**: auto-post, contact group sync, group sender.

## Migration from Baileys

Auth state format is incompatible. Devices already paired on Baileys must scan QR / enter pairing code once on first run after switching backends.
