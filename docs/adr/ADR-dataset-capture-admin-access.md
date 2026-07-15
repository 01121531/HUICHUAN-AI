# ADR: Dataset Capture Policy, Access, and Administration

## Status

Accepted and implemented on 2026-07-15. This decision supersedes the first-release global `DatasetCaptureAdminVisible` design.

## Context

Dataset capture contains complete prompts, tool payloads, multimodal inputs, and model outputs. Runtime capture policy, operator authorization, query metadata, training exports, and destructive lifecycle operations therefore require separate ownership and explicit audit boundaries.

The administration UI must work with users and capture records rather than exposing JSONL files, paths, or row numbers. JSONL remains the durable training format and export contract.

## Domain Model

- **Capture policy**: immutable runtime snapshot containing `enabled`, model mode, and selected site model names.
- **Capture permission**: per-administrator `dataset_capture.view` and `dataset_capture.download` grants. Download depends on view.
- **Capture user**: the real site user resolved at query time, including deleted-user fallback labels.
- **Capture record**: one complete successful model call with a stable opaque capture ID.
- **Conversation partition**: one JSONL file owned by a `(node, user, token, session)` tuple.
- **Capture index**: rebuildable, non-content metadata used for aggregation, filtering, locating records, export, and deletion.
- **Export selection**: current filtered result, selected users under the current filter, and/or selected records.
- **Audit event**: non-content management log for view, download, delete, policy update, or permission update.

## Decisions

1. Runtime policy and human access are independent. System Settings controls whether capture runs and which requested site models are eligible. User Management controls which individual administrators may view or download historical data.
2. Root is an implicit superuser. Administrators have no dataset capture access by default. Ordinary users can never access administration APIs.
3. `dataset_capture.download` is invalid without `dataset_capture.view`. The backend validates the dependency even if the UI already enforces it.
4. The legacy `DatasetCaptureAdminVisible=true` option is migrated once by granting existing administrators view and download. A migration marker prevents later runs from restoring revoked permissions. The legacy option no longer participates in authorization.
5. Model admission uses the client-requested site model before channel mapping. The training record's `model` remains the effective upstream model after mapping.
6. Policy updates use a typed Root-only API and are applied atomically as one immutable snapshot. Selected-model mode cannot save an empty list. Selected models that later go offline remain visible as unavailable until Root removes them.
7. New records use `node-<node>/user-<id>/token-<id>/session-<session>.jsonl`. Storage scope is excluded from the fixed eleven-field training schema.
8. `dataset_capture_indices` stores only opaque file/record locators, user and token IDs, token scope, actual group, requested/effective model, final successful channel ID, session ID, timestamp, and byte size. It stores no prompt, response, credential, query string, or absolute path.
9. The index is rebuilt from trusted capture roots at startup. Missing files remove stale index rows; discoverable historical records are backfilled. Metadata unavailable in legacy JSONL remains unknown rather than fabricated.
10. The page aggregates by username. It supports time, effective model, token, actual group, final channel ID, username, and content filters. Content search first narrows by indexed metadata, then reads at most 5,000 candidate records without creating a persistent plaintext full-text index.
11. List APIs return summaries only. Opening a detail reads the complete record through the server-side file allow-list and emits `dataset_capture.view`.
12. Export selection is evaluated again by the server. User selection means all records for that user under the submitted filter. User and record selections are ORed within the selection and ANDed with the filter; overlaps are deduplicated.
13. Export order is user, token, session, capture time, and row. The server writes a `0600` temporary file, validates every line against the fixed schema, and sends it only after the whole export succeeds. All selected users still produce one JSONL file.
14. Export authorization requires both view and download before generation and is rechecked before delivery. Successful delivery emits one `dataset_capture.download` audit with counts, time/model conditions, and byte size, never content keywords or record bodies.
15. Delete remains Root-only and accepts selected opaque capture IDs. Each selected ID resolves to its complete conversation file; overlapping selections delete that conversation once. Every successful conversation deletion emits its own `dataset_capture.delete` audit.
16. Writer append, index callback, file deletion, and deletion index cleanup share the capture-file lock. This prevents deletion from racing between a committed line and its index callback.
17. Client-supplied paths are never accepted. Every read, export, and delete resolves opaque IDs from a fresh server-side allow-list derived from the configured trusted path template.
18. JSONL files are storage and export artifacts only. The page never displays file names, paths, or source rows.

## Audit Metadata

| Action | Trigger | Allowed metadata |
|---|---|---|
| `dataset_capture.view` | Complete record detail opened | operator, capture ID, user ID, token ID, node, result |
| `dataset_capture.download` | Combined export delivered | operator, user count, record count, time/model filter, bytes, node |
| `dataset_capture.delete` | Conversation deleted | operator, session ID, selected count, deleted count, node |
| `dataset_capture.permission_update` | Root changes administrator grants | target administrator ID and before/after grants |
| `dataset_capture.policy_update` | Root changes capture policy | enabled state, mode, model count, change summary |

Audit metadata must not contain content search terms, system prompts, messages, responses, tool inputs, credentials, API keys, cookies, or absolute paths.

## Rejected Alternatives

- **Global administrator visibility switch**: rejected because it cannot express least-privilege access for individual administrators.
- **Using effective upstream model for policy admission**: rejected because Root configures the public site model catalog, not provider-specific mapping results.
- **Displaying or selecting JSONL files in the page**: rejected because storage layout is not the administration domain model.
- **Adding username, token, group, or channel fields to exports**: rejected because it would break the fixed training schema.
- **Persistent plaintext full-text index**: rejected because it duplicates sensitive capture content and increases disclosure surface.
- **Deleting one JSONL line in place**: rejected because rewriting live conversation files adds corruption and writer race risk. Deletion is conversation-scoped.

## Invariants

- Disabling capture or changing model scope affects only new requests and never deletes history.
- Failed attempts, non-2xx responses, cancelled clients, incomplete streams, normalization failures, and schema failures never enter the main dataset.
- Every newly captured file contains exactly one user, token, and session.
- Storage metadata never changes the fixed eleven-field serialized record.
- Successful content disclosure and destructive actions create non-content audit events.
- Administrators cannot obtain delete capability through permission overrides.
- No page or API operation trusts a client-supplied filesystem path.
