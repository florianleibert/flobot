# Research round 16 — Local serving on Apple silicon: what actually constrains a local agent

**Date:** 2026-11-04
**Track:** D — local serving, routing, evaluation
**Sources (fetched 2026-11-04):**
- https://github.com/ml-explore/mlx-lm (README, MIT)
- https://github.com/ml-explore/mlx-lm/blob/main/mlx_lm/SERVER.md (server surface)
- flobot's own `internal/amesh/` (mesh discovery) for the local comparison

---

## 1. The serving knobs that decide whether a local agent is usable

mlx-lm exposes exactly five things that matter to an agent harness:

| Knob | Default | What it does | Agent consequence |
|---|---|---|---|
| `--max-kv-size n` | unset (grows) | rotating fixed-size KV cache | caps RAM; **degrades quality** below the working set. 512 = "very little RAM but worse quality"; 4096+ = better |
| `--prefill-step-size n` | **2048** | how many prompt tokens processed at once | smaller = less peak memory during prefill, slower prompt processing |
| `--prompt-cache-file` | none | cached prompt treated as a **prefix** to the supplied prompt | the single biggest latency win for a stable system prompt |
| `--temp`, sampling args | model default | — | — |
| `trust_remote_code` | prompts interactively | tokenizer/model code execution | **must be explicit in a daemon**; an interactive prompt in a server context is a hang |

The `trust_remote_code` detail is a real operational trap: mlx-lm prompts on the terminal when the flag is absent. A flobot-managed server started without a TTY will block forever. Any supervisor must pass the flag explicitly or refuse the model.

## 2. Prompt caching is the architecture, not an optimisation

> "Caching prompts can substantially speedup reusing the same long context with different queries… The cached prompt is treated as a prefix to the supplied prompt. Also notice when using a cached prompt, the model to use is read from the cache and need not be supplied explicitly."

Two consequences for flobot's design:

1. **A stable system-prompt prefix is worth engineering for.** This is the same conclusion Hermes reached from the other direction (round 10): they froze the memory snapshot mid-session *specifically* to preserve the prefix cache. Two independent systems converge on: **anything that varies per turn must go after the stable prefix.**
2. **The cache carries the model identity.** A cached prompt file is bound to a model. If flobot swaps routes, the cache must be invalidated — a stale cache silently runs the wrong model.

flobot today re-reads `Bot.Memory` and rebuilds the whole prompt per run, so it never gets prefix reuse. The fix is ordering: stable prefix (system + skills index + memory) → volatile tail (run context, handoff payload).

## 3. Wired memory — the local-only failure mode

> "Models which are large relative to the total RAM available on the machine can be slow. mlx-lm will attempt to make them faster by wiring the memory occupied by the model and cache. This requires macOS 15 or higher… If you see `[WARNING] Generating with a model that requires ...` then the model will likely be slow on the given machine. If the model fits in RAM then it can often be sped up by increasing the system wired memory limit: `sudo sysctl iogpu.wired_limit_mb=N`."

This is a capability fact flobot's router must know: **model choice is bounded by machine RAM and the wired limit**, and the failure mode is not an error — it is silent slowness. A router that picks a 70B model on a 64 GB Mac "works" and is unusable.

The router needs a per-node capacity fact: total RAM, wired limit, and the largest model known to run acceptably. That is exactly what `internal/amesh` should carry and currently does not.

## 4. Batch generation and distributed inference

- `batch_generate` exists for efficient batched continuations — relevant if flobot ever runs several bots concurrently against one local server.
- `mx.distributed` for distributed inference/fine-tuning across machines. Not needed for flobot's per-bot model choice, but it means a mesh of Macs *can* serve one large model, which changes the capacity calculus for a "big model" route.

## 5. The local-server checklist flobot must implement

Derived directly from the above:

1. **Never start a model without explicit `trust_remote_code`** (or a refusal), because a TTY prompt hangs a daemon.
2. **Persist a prompt-cache file per (bot, route, system-prompt-hash)**; invalidate on any of those changing.
3. **Record prefill step size and max-kv-size per route** so quality/RAM tradeoffs are explicit and reproducible.
4. **Probe capacity before routing**: RAM, wired limit, model size; refuse or warn rather than run slowly.
5. **Order prompts stable-prefix-first** so caching actually hits.
6. **Surface the `[WARNING] Generating with a model that requires ...` line** as a UI warning — it is the only signal that a run will be slow.
7. **Treat batch_generate as the concurrency primitive** if multiple bots share one server.

## 6. What this means for "a better Grok Bot"

Grok Bot hides the model entirely (round 7: "no model picker"). That is only possible because they own the hardware. flobot's differentiator is that it runs on hardware you own — which means **the model choice is a first-class user decision with real physical consequences**. The product must therefore make capacity legible:

- show what each route costs in RAM and tokens/sec on *this* machine
- refuse routes that cannot fit
- show when a route is running degraded (rotating KV cache, unwired memory)
- let a bot be pinned to a route and show that pin everywhere

None of that exists in Grok Bot because none of it can.

## 7. Gap vs flobot today

| Capability | flobot today | Gap |
|---|---|---|
| Prompt cache reuse | none — prompt rebuilt per run | missing |
| Stable-prefix prompt ordering | no (memory first, then everything) | missing |
| Capacity facts per node | amesh health only | no RAM/wired/model-fit |
| Degraded-mode detection | none | missing |
| Explicit trust_remote_code | n/a (flocode child owns it) | must be asserted |
| Per-route prefill/kv settings | none | missing |
| Concurrency via batch | one child per run | missing |
