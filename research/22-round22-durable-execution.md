# Research round 22 — Durable execution: the four timeouts, heartbeats, and replay

**Date:** 2026-11-04
**Track:** F — durable execution, memory, sandboxing
**Sources (fetched 2026-11-04):**
- https://docs.temporal.io/workflows (Workflow replay)
- https://docs.temporal.io/encyclopedia/detecting-activity-failures (four timeouts + heartbeats)
- Hermes' admission-recovery model (round 12 §1.3) as the lightweight comparison

---

## 1. The core insight: replay from an event log, not a memory snapshot

> "When it's time to continue the Workflow, Temporal doesn't restore memory from a snapshot. It starts the Workflow code from the beginning, replays the Event History step by step, and uses that history to guide the code back to the exact state as before. So the Workflow code is re-run, but uses the recorded events instead of redoing work."

And the constraint that follows:

> "Because the Workflow is re-executed to rebuild its state: It has to make the same decisions when given the same history. It shouldn't depend on any values not recorded in the history which would be different between runs."

Three named hazards: `Date.now()`, randomness, and un-recorded network calls. Their fix is replay-safe APIs — time read from context, timers recorded as events, randomness captured once.

**The division of labour is the whole architecture:**

> "Activities handle everything that interacts with the outside world, like: API calls, Database queries, **LLM invocations**, File I/O. When a Workflow calls an Activity, the Activity runs once, its result is recorded in the Event History. During replay, that result is reused, not recomputed."

So: **deterministic orchestration + recorded side effects.** The workflow may be replayed arbitrarily; the activity runs once and its result is memoised.

## 2. Why flobot cannot use this model directly — and what it should take instead

flobot's unit of work is `exec flocode` as a child process (`internal/executor/executor.go`). That child is *not* deterministic and *not* replayable: it is an LLM agent loop with side effects. You cannot re-run it to rebuild state.

But flobot's *orchestration* is deterministic: bot → skill → route → prompt → child → parse events → record. So the correct split for flobot is:

| Layer | Determinism | Durability strategy |
|---|---|---|
| orchestration (which bot, which route, which prompt) | deterministic | replayable from a run record |
| child agent loop (LLM + tools) | non-deterministic | **memoise the whole run as one activity** |
| individual tool calls inside the child | non-deterministic | recorded in the event stream, not replayable |

That means flobot's durable unit is the **run**, and its event log is an *audit* log rather than a replay log. The achievable guarantee is: **a run either completes and is recorded, or is recorded as incomplete with enough state to resume from a checkpoint** — not "replay produces identical state."

## 3. The four timeouts, and which ones flobot needs

| Timeout | Definition | Retryable? | flobot analogue |
|---|---|---|---|
| **Schedule-To-Start** | scheduled → worker picks it up (queue wait) | **non-retryable by design** — "a retry would place the Activity Task back into the same Task Queue" | queue wait before a run starts |
| **Start-To-Close** | max time for one attempt | yes, per attempt | per-run wall clock |
| **Schedule-To-Close** | max time for the whole execution incl. all retries | bounds total duration without changing max attempts | per-routine total budget |
| **Heartbeat Timeout** | max gap between heartbeats | yes | child liveness |

The Schedule-To-Start non-retryability is a genuinely subtle point: a queue-wait timeout must not be retried because retrying re-enqueues into the same backed-up queue. flobot's `internal/queue` has no such distinction; a stuck queue would retry forever.

The three-way split (queue wait / attempt / total) is exactly what flobot's single `timeout` value should become. One number cannot express "wait at most 30 s to start, run at most 10 min per attempt, give up after 30 min total."

## 4. Heartbeats carry progress payloads — this is the resume mechanism

> "A Heartbeat can include an application layer payload that can be used to save Activity Execution progress. If an Activity Task Execution times out due to a missed Heartbeat, the next Activity Task can access and continue with that payload."

And: "Activity Cancellations are delivered to Activities from the Temporal Service when they Heartbeat. **Activities that don't Heartbeat can't receive a Cancellation.**"

Two consequences flobot should adopt:

1. **Heartbeat payload = checkpoint.** A long child run should periodically emit a structured progress record (files touched, steps done, current plan). On timeout, the next attempt resumes from that payload instead of restarting.
2. **Cancellation requires a heartbeat channel.** flobot's stop is SIGTERM to the child (inventory). A cooperative cancel needs a channel the child polls — which is what a heartbeat is.

Throttling detail worth knowing: heartbeat interval is throttled to `min(heartbeatTimeout * 0.8, maxHeartbeatThrottleInterval)` with defaults 30 s / 60 s, and "Throttling does not apply to the final Heartbeat message in the case of Activity Failure."

And `0s` semantics are documented precisely because they are counter-intuitive: "0s means the Heartbeat Timeout is disabled. No Heartbeat timer is started… It does not mean an unbounded timeout, and it does not mean the Activity times out immediately." Also capped at Start-To-Close.

## 5. What "should heartbeat" means, in their words

> "Heartbeating is best thought about not in terms of time, but in terms of 'How do you know you are making progress?'"

Suitable: reading a large file from S3; running an ML training job on local GPUs.
Not suitable: a quick API call; reading a small file.

flobot's runs are LLM agent loops — minutes long, many tool calls. They are squarely in the "should heartbeat" category.

## 6. The minimal durable-run design for flobot

Concretely, using only what flobot already has (SQLite store + event stream):

```
runs table gains:
  fire_id          TEXT UNIQUE   -- idempotency key for the trigger occurrence
  attempt          INTEGER       -- 1-based
  parent_run_id    TEXT          -- retry/resume lineage
  checkpoint       TEXT          -- last heartbeat payload (JSON)
  checkpoint_at    TIMESTAMP
  timeout_start    INTEGER       -- schedule-to-start budget (s)
  timeout_attempt  INTEGER       -- start-to-close budget (s)
  timeout_total    INTEGER       -- schedule-to-close budget (s)
  terminal_reason  TEXT          -- completed | failed | timed_out_attempt |
                                 --   timed_out_total | cancelled | unknown_delivery
```

Rules:

1. **`fire_id` is written before spawn.** A crash after write but before spawn leaves `pending`; recovery re-spawns with the same `fire_id`. A duplicate trigger with the same `fire_id` is a no-op.
2. **Heartbeat writes `checkpoint`.** The child emits progress events; the executor persists the latest as `checkpoint`. Cheap: it is already parsing events.
3. **On attempt timeout:** if `attempt < max_attempts` and a checkpoint exists → new attempt with `parent_run_id` set and the checkpoint injected into the prompt.
4. **On total timeout:** terminal `timed_out_total`, no further attempts.
5. **On cancel request:** write `cancel_requested`; deliver via the heartbeat channel; escalate to SIGTERM then SIGKILL after grace (round 12 §1.7's exact semantics).
6. **Ambiguous after send:** if an outbound side effect was claimed but not confirmed, terminal `unknown_delivery` and **do not retry automatically** (round 12 §1.3).

Step 6 is the one that prevents duplicate emails/comments, and it is the single most valuable rule here.

## 7. What flobot should explicitly not build

- **Deterministic workflow replay.** Requires rewriting the agent loop as deterministic code with recorded effects. Enormous cost, and the child process is a black box flobot does not control.
- **A workflow DSL.** flobot's orchestration is one step deep (trigger → run). A DSL for one step is ceremony.
- **Distributed task queues.** Single machine.

The transferable content is: four timeouts instead of one, fire identity for idempotency, heartbeat-as-checkpoint, cooperative cancel via heartbeat, and fail-safe-to-unknown on ambiguous delivery.

## 8. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Fire identity / idempotency key | none | missing |
| Attempt counter + lineage | none | missing |
| Three-way timeout split | one timeout | missing |
| Heartbeat channel | none | missing |
| Checkpoint payload | none | missing |
| Resume from checkpoint | none | missing |
| Cooperative cancel | SIGTERM only | missing |
| Ambiguous-delivery state | none | missing |
| Event log as audit | ✓ events table | — |
