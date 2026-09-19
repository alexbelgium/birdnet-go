# Species workspace: scientific-name aliases and canonical identity

This note documents a known limitation of the species workspace introduced by the stacked workspace PR. It is intentionally documentation-only so a later implementation/review can address taxonomic aliases consistently across the feature instead of fixing one endpoint in isolation.

## Current behavior

The workspace currently treats a scientific name as the species identity used by its datastore queries.

In the v2 datastore, multiple label rows that resolve to the same scientific name are combined. This correctly handles the same species emitted by multiple models, plus legacy `Scientific_Common` labels for that same scientific name.

However, different scientific names that are taxonomic synonyms are not currently merged by the workspace. For example, if stored detections use an older name and a newer accepted name for the same biological species, they may appear as separate workspace species and are queried separately.

OpenFauna already has a narrower alias concept for display/localization: name resolution first tries the exact scientific name and can then fall back to an unambiguous canonical alias. That does not currently define datastore identity for the workspace.

## Why this must be solved globally

Applying alias expansion to only one endpoint would create inconsistent and potentially dangerous behavior. For example, if `GET /species/stats?species=...` counted both an accepted name and a synonym while `POST /species/delete` deleted only the exact workspace name, the confirmation counts would no longer match what deletion actually removes.

Any future canonical-species layer must therefore be applied consistently to at least:

- species inventory / grouping
- species-specific statistics
- recordings listing
- best-recording selection
- delete confirmation and deletion
- include / exclude / confirmed memberships
- external-link identity where relevant
- row navigation and species-page URLs

The same canonicalization contract should also be shared by both the legacy and v2 datastores.

## Required invariants for a future implementation

1. A biological species represented by multiple supported scientific-name aliases must have one canonical workspace identity.
2. All aliases mapped to that identity must contribute to the same counts, first/last seen values, review statistics, recordings and best-recording selection.
3. Deletion and its confirmation must operate on exactly the same alias set.
4. Locked detections remain protected regardless of which alias they were stored under.
5. Membership toggles must not create contradictory state for different aliases of the same species.
6. Existing multi-model behavior must remain intact: several model label rows with the same scientific name are still one species.
7. Subspecies/prefix-sharing names must not be accidentally merged. Existing protections such as keeping `Motacilla alba alba` distinct from `Motacilla alba` must remain.
8. Canonicalization must be deterministic and stable across restarts; it must not depend on the currently selected UI language.
9. Historical detections should remain queryable even if the preferred accepted name changes in a newer taxonomy snapshot.
10. API responses should expose enough information to distinguish the canonical identity from stored/source labels if that becomes necessary for debugging or migrations.

## Design question for review

Before implementation, decide where the canonical alias map belongs. A good solution should avoid duplicating OpenFauna logic while also avoiding a hidden dependency where display-name fallback silently changes destructive datastore operations.

One possible direction is a dedicated taxonomy identity resolver that exposes explicit operations such as:

- canonical scientific name for workspace identity
- all recognized scientific-name aliases for that identity

The workspace datastore methods would then receive or resolve that alias set consistently. OpenFauna's display/localization resolver could consume the same taxonomy source but remain logically separate from destructive/query identity.

## Relationship to the species-specific stats optimization

A species-filtered stats endpoint such as `GET /species/stats?species=<name>` should initially preserve the workspace's current identity semantics: aggregate all model labels for the requested scientific name, but do not independently expand taxonomic synonyms.

Alias support should be introduced only when the complete workspace identity contract above is implemented, so stats, recordings and deletion cannot disagree.

## Suggested Claude review focus

When this is implemented, review specifically for:

- alias symmetry across every workspace endpoint
- old-name/new-name migrations and historical data
- multi-model labels combined without duplicate counting
- locked-row race safety across aliases
- prefix/subspecies false matches
- MariaDB and SQLite query plans for alias sets
- membership canonicalization and backwards compatibility with existing config values
- external links and localized display names not leaking into datastore identity
