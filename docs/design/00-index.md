# Architecture Design Documents

> 设计文档集 — 在编码前必须完成的内容

## Status

🚧 **Drafting.** Will be populated after requirements are fully frozen.

## Document Index

| # | Document | Status | Purpose |
|---|---|---|---|
| 01 | Data Model | ✅ Draft v0.1 | Entity definitions + ER diagram + DB schema |
| 02 | Module Layout | ✅ Draft v0.1 | Backend / Android / shared packages + boundaries |
| 03 | WebSocket Protocol | ✅ Draft v0.1 | Message frame format + routing rules + error handling |
| 04 | State Machines | ✅ Draft v0.1 | Room / Participant / Agent lifecycle diagrams |
| 05 | Security Model | ⏳ pending | Auth, TLS, room access control, agent sandboxing |
| 06 | Deployment | ⏳ pending | systemd unit, Nginx config, VPS provisioning |
| 07 | Three-Sages Coalition (EVA MAGI) | 📝 drafted | MVP composite-agent design: D22/D23/D24/D25 — roles, R1→R2→R3→Synth protocol, voting threshold, room announcement |

## Reference

- Requirements: [`../requirements/00-overview.md`](../requirements/00-overview.md)
- Tech stack rationale: [`../requirements/01-tech-decisions.md`](../requirements/01-tech-decisions.md)
- Public API: [`../api/openapi.yaml`](../api/openapi.yaml) — REST + WebSocket contract