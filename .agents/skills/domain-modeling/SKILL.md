---
name: domain-modeling
description: Model domains before implementing cross-cutting behavior. Use when a feature has multiple actors, permissions, lifecycle states, audit requirements, storage boundaries, or business invariants that need a shared glossary and enforceable design.
---

# Domain Modeling

Describe the smallest domain model that makes the requested behavior explicit and testable.

## Workflow

1. Identify actors, entities, value objects, policies, commands, queries, domain events, and external systems from observed code and requirements.
2. Define a glossary in the ADR or under `docs/domain/`; use the same terms in APIs, code, UI, tests, and audit records.
3. Write invariants as executable rules, especially authorization, ownership, immutability, and complete-or-nothing persistence.
4. Describe state transitions and their triggers. Include disabled, denied, incomplete, malformed, cancelled, and failed states.
5. Assign each rule to a concrete enforcement boundary: middleware, controller/service, repository/filesystem, frontend visibility, or asynchronous writer.
6. Keep read models separate from mutation models. Avoid exposing filesystem paths, credentials, request headers, or raw identities as domain identifiers.
7. Map each domain event to its audit metadata and explicitly exclude sensitive content.
8. Finish with acceptance criteria and tests that prove the invariants.

## Output Shape

Use concise tables or lists for:

- glossary and ownership;
- actor-capability matrix;
- commands, queries, and audit events;
- state transitions;
- invariants and enforcement points;
- unresolved decisions and assumptions.

Prefer existing repository concepts over introducing new aggregates or persistence layers.
