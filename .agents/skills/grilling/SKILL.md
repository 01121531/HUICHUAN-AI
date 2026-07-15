---
name: grilling
description: Relentlessly resolve ambiguous product and architecture decisions before implementation. Use for cross-cutting features involving permissions, sensitive data, audit trails, storage, lifecycle, or unclear ownership; record conclusions in ADRs as the discussion progresses.
---

# Grilling

Turn an incomplete request into explicit, testable decisions without losing delivery momentum.

## Workflow

1. Inspect the existing code and documentation before asking questions.
2. List only ambiguities that can materially change security, data ownership, compatibility, or architecture.
3. For each ambiguity, state the observed facts, viable choices, recommendation, and consequence.
4. Ask the user only when the choice cannot be inferred safely. When execution has already been requested, apply conservative defaults and record them as assumptions.
5. Use the `domain-modeling` skill to identify entities, policies, events, state transitions, and invariants.
6. Create or update an ADR under `docs/adr/` while decisions are made. Include context, decisions, rejected alternatives, consequences, and acceptance criteria.
7. Continue until every acceptance criterion maps to an implementation point and a verification step.

## Required Coverage

For sensitive-data features, resolve at minimum:

- authorization at both UI and API boundaries;
- data scope and tenant/node ownership;
- read, preview, export, and deletion semantics;
- audit event names and non-sensitive metadata;
- enable/disable behavior and historical-data behavior;
- pagination, size limits, concurrency, malformed data, and path traversal;
- failure behavior and operational observability.

Do not treat menu visibility as authorization. Do not put secrets or captured payload contents into audit logs.
