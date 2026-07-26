# Technology Decisions

## Final Stack

| Layer | Choice | Rationale |
|---|---|---|
| Language | **Go 1.22+** | Low memory footprint (15MB/process vs Python's 200MB+); goroutines native fit WebSocket fan-out; single-binary deploy matches small VPS |
| Web framework | **Gin** | Largest ecosystem, mature, performance-tuned |
| ORM | **sqlc** | Type-safe SQL via code-gen; closest to MyBatis feel for Java background; no ORM abstraction tax |
| Database | **Postgres 15+** | JSONB for room config, full-text search ready, mature replication |
| Migrations | **golang-migrate** | Standard Go migration tool |
| WebSocket | **gorilla/websocket** | Battle-tested, most widely used |
| Validation | **go-playground/validator** | Struct tag-based, same model as Java Bean Validation |
| Logging | **log/slog** | Go 1.22 standard library, structured JSON native |
| CLI | **cobra** | Powers kubectl/hugo; `fsc` command family |
| Deployment | **systemd + Nginx** | No Docker in MVP; single binary + reverse proxy |

## Stack Comparison: Go vs Python

User raised concern about VPS resource constraints. Comparison:

| Metric | Python FastAPI | Go |
|---|---|---|
| RSS per process | 200 MB | 15 MB |
| Concurrent WebSocket | ~500 | 10,000+ |
| Memory per connection | ~400 KB | ~5 KB |
| Cold start | 1-2s | 50ms |

**Decision**: Go chosen. On a 2GB VPS, Python caps at ~8 workers / 500 connections. Go handles 100+ workers / 1000+ connections with headroom.

## Why Not Redis (Yet)

MVP deletes Redis. Online status is derivable from live WebSocket connections. Message bus is single-process asyncio-equivalent (channels). Will revisit when:
- Horizontal scaling needed
- Persistent message queue required
- Hot cache emerges

YAGNI principle applied.

## Why Not ORM (Hibernate-style)

`sqlc` chosen over `GORM` because:
- sqlc generates type-safe code from raw SQL — closer to MyBatis than Hibernate
- Zero runtime overhead (no reflection)
- Schema-as-source-of-truth: SQL files checked into repo

If user prefers Hibernate-style ORM API, can swap to GORM without breaking business logic (interface layer between DAO and services).