# backend-api

Public BFF and deterministic business-workflow service for the Evidence-First
3PL Invoice Reconciliation Engine. It owns ingestion orchestration, pricing,
reconciliation, audit events, and human-review state. It does **not** run OCR
or make AI-based financial decisions.

## Phase 0

Implemented now:

- Gin API with `GET /healthz` and `GET /readyz`
- Supabase REST readiness client
- Worker scaffold for Supabase-backed jobs
- Container, CI, and safe environment configuration

Document upload, duplicate detection, pricing, and reconciliation begin only
after the versioned schemas and synthetic ground truth are ready.

## Local development

```bash
copy .env.example .env
go mod download
go run ./cmd/api
go test ./...
```

`/readyz` requires `SUPABASE_URL` and `SUPABASE_PUBLISHABLE_KEY` and checks
the Supabase REST endpoint. Do not put credentials in this repository.

For the full local stack, combine this repository's override with the Compose
file in the sibling `infra` repository:

```bash
docker compose -f ../infra/docker-compose.yml -f docker-compose.dev.yml up --build
```

## Repository boundaries

- `schemas`: versioned canonical JSON Schemas
- `infra`: Supabase migrations, Storage/RLS policy, Compose, runtime config
- `synthetic-data`: benchmark fixtures and document generator
- `ai-service`: classification, OCR, extraction, signature presence detection
- `frontend`: reviewer-facing Next.js application

The backend will use Supabase signed URLs to give `ai-service` temporary
read-only access to document objects in Phase 4.
