# Research round 23 — Memory systems: the three tiers, and what to store where

**Date:** 2026-11-04
**Track:** F — durable execution, memory, sandboxing
**Sources (fetched 2026-11-04):**
- https://www.sqlite.org/fts5.html (FTS5 reference)
- Hermes' memory model (round 10) and session-search comparison table
- Grok Bot's three-layer memory (round 7 §3)
- flobot's `internal/store/store.go` (single capped note)

---

## 1. The convergence: every mature product lands on three tiers

| Tier | Grok Bot | Hermes | flobot today |
|---|---|---|---|
| **Always-in-prompt** | per-bot memory + shared facts | `MEMORY.md` 2,200 ch + `USER.md` 1,375 ch | one capped note |
| **Searchable archive** | conversation history (chat-only) | FTS5 over `state.db`, unlimited, free | `runs`/`events` tables, no search |
| **Artifacts** | files/attachments | skill `references/` | workspace files |

The budgets are the interesting part. Hermes publishes them: ~1,300 tokens total, fixed. Grok Bot publishes no budget but says memory "develops over time" and is agent-curated with no UI. flobot clips silently.

**The rule all three imply:** the always-in-prompt tier must be small, bounded, and *visible*; everything else must be retrievable on demand. A memory system that grows without bound in the prompt is a slow-motion failure.

## 2. FTS5 is the right archive engine, and the details matter

flobot already uses SQLite. FTS5 is compiled into the standard amalgamation since 3.9.0, so there is no dependency to add.

### The table shape

```sql
CREATE VIRTUAL TABLE messages_fts USING fts5(
  body,
  role UNINDEXED,
  run_id UNINDEXED,
  bot_id UNINDEXED,
  tokenize = 'porter unicode61'
);
```

- `UNINDEXED` columns are stored but not searchable — correct for ids you filter on, not search.
- `tokenize = 'porter unicode61'` gives stemming (so "deploying" matches "deploy") plus Unicode case folding. The default `unicode61` alone does not stem.
- `prefix='2 3'` adds prefix indexes so `dep*` is a lookup instead of a range scan. Worth it for interactive search.

### The query syntax flobot gets for free

| Feature | Syntax | Use |
|---|---|---|
| phrase | `"one two three"` or `one + two + three` | exact phrase match |
| prefix | `thr*` | typeahead |
| initial token | `^one` | starts-a-message-with |
| NEAR | `NEAR("one two" "three four", 10)` | proximity, default N=10 |
| column filter | `colname : query`, `{a b} : query`, `-colname : query` | search only bodies |
| boolean | `NOT` > `AND` > `OR`; implicit AND between adjacent terms | — |

Two syntax traps documented in the spec that flobot must handle in its query builder:

1. **`"one two thr*"` "may not work as expected"** — inside double quotes the `*` is passed to the tokenizer and likely discarded. Prefix must be outside the phrase: `"one two" + thr*`.
2. **Implicit AND groups tighter than NOT**: `'one NOT two three'` parses as `one NOT (two AND three)`, not `(one NOT two) AND three`.
3. **`(one OR two) three` is a syntax error** — implicit AND is never inserted adjacent to parentheses.

A user-facing search box must escape barewords (only ASCII alnum, `_`, and non-ASCII are barewords; everything else needs quoting) or a search for `foo-bar` throws.

### Ranking and snippets

- `ORDER BY rank` → bm25 by default.
- `highlight(tbl, col, '<b>', '</b>')` → matched terms wrapped.
- `snippet(tbl, col, '<b>', '</b>', '…', 20)` → a fragment around the match, 20 tokens.

`snippet()` is what makes search results renderable in a list without loading full messages.

### Contentless vs external-content

For flobot the right choice is **external content**: the messages already live in a real table, and an FTS index over them avoids duplicating text.

```sql
CREATE VIRTUAL TABLE messages_fts USING fts5(
  body, content='messages', content_rowid='id'
);
```

Pitfalls the spec calls out explicitly ("External Content Table Pitfalls"): external-content tables do **not** stay in sync automatically. You must maintain them with triggers:

```sql
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.id, old.body);
END;
CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES('delete', old.id, old.body);
  INSERT INTO messages_fts(rowid, body) VALUES (new.id, new.body);
END;
```

The `'delete'` special command requires the *old* values — that is why the delete trigger passes `old.body`.

Also: `'rebuild'` regenerates the index from content (`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`) — the migration path when adding FTS to an existing DB. And `'integrity-check'` verifies consistency.

## 3. What goes in each tier — a concrete policy for flobot

| Content | Tier | Budget |
|---|---|---|
| operator preferences, style, standing rules | always-in-prompt (`USER` note) | ~500 tok |
| bot's own lessons, environment facts | always-in-prompt (`MEMORY` note) | ~800 tok |
| dated work log ("what we did") | **searchable archive**, summarized into memory weekly | unbounded |
| skill index (name + description) | always-in-prompt | ~3k tok (round 11) |
| skill bodies | on demand via tool | unbounded |
| run transcripts / events | archive, FTS5 | unbounded |
| files produced | workspace artifacts | disk |

The dated work log is the piece flobot is missing entirely and it is the cheapest to add: it is a projection over `runs` (bot, timestamp, one-line outcome). Grok Bot calls it "a dated log of what it has worked on with you" and it is what makes memory feel like continuity rather than a settings field.

## 4. The write path must be server-authoritative

From round 10 §10's documented failure: small models claim "I've saved that to memory" without calling the tool. flobot's defence is structural — it already owns the store. So:

1. Memory writes are **HTTP endpoints** (`POST /api/bots/{id}/memory`), not model-asserted facts.
2. The agent's memory tool call goes through the same endpoint; the *store write* is the confirmation.
3. The UI shows memory as data read from the store, never as text parsed from a reply.
4. A post-turn review pass (round 10 §6) proposes entries; they land only if the write succeeds.

That inverts Hermes' problem: they must trust the model's tool call; flobot can verify it.

## 5. Retrieval: when to search vs when to remember

The decision rule that keeps prompts small:

```
if the fact is needed on every turn        → always-in-prompt tier
else if the fact is needed for this task   → retrieve by FTS5 before building the prompt
else                                        → leave in archive
```

Step 2 is the one nobody implements and it is cheap: before spawning a child, run one FTS5 query built from the run's prompt against the archive; inject the top-k snippets (with `snippet()`) as a "possibly relevant history" block. That is retrieval-augmented prompting with zero new infrastructure and no embeddings.

Cost: one SQLite query (~20 ms per Hermes' measurement), a few hundred tokens of snippets per run. Benefit: memory can stay at 1,300 tokens forever while the agent still has access to everything.

## 6. What not to build

- **Vector embeddings.** For a single user's own history, FTS5 + a date filter beats embeddings on cost, latency and debuggability. Embeddings become worth it at corpus sizes and query shapes flobot will not hit.
- **Automatic summarisation of everything.** Summaries lose exactly the details that make history useful ("the fix was in line 42"). Keep raw text searchable; summarise only into the bounded memory tier.
- **A memory admin screen.** Grok Bot deleted theirs (round 7 §3). Chat-editable + searchable beats a CRUD table.

## 7. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Bounded always-in-prompt memory with visible usage | silent clip | missing |
| Dated work log | none | missing |
| FTS5 over messages/events | none | missing |
| Retrieval before prompt build | none | missing |
| Server-authoritative memory writes | HTTP only (no agent tool) | half-present |
| Snippet rendering in search results | none | missing |
| Search query escaping | n/a | must build |
