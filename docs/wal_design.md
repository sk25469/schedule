# Write-Ahead Log (WAL) – Design Notes (v0)

This document captures the **current, agreed-upon design decisions** for the scheduler WAL.

It intentionally does **not** specify concrete formats or implementations yet.
Its purpose is to lock down **semantics, guarantees, and failure contracts** before code exists.

If a future change contradicts this document, it must be justified explicitly.

---

## 1. Purpose of the WAL

The WAL is the **single source of truth** for the scheduler.

It exists to:

* serialize all authoritative state transitions
* allow deterministic reconstruction of coordinator state after crashes
* define a total order of history

The WAL is **not**:

* an audit log
* a metrics stream
* a debugging trace

Only events that affect **task truth or ownership** belong here.

---

## 2. Authority Model

* The Coordinator is the only authority that may append to the WAL
* Workers never write to the WAL directly
* Worker requests are advisory until accepted and serialized

All in-memory state must be:

* derived from WAL replay, or
* explicitly marked as soft / lossy

If a piece of state cannot be rebuilt from the WAL, it is **not authoritative**.

---

## 3. Ordering Guarantees

The WAL defines a **global, total order** over all events:

* task lifecycle events
* lease lifecycle events

Guarantees:

* replaying the WAL prefix reconstructs exact coordinator state
* no per-task or per-worker sub-logs exist

Tradeoff:

* single serialization point
* reduced throughput (acceptable for v1)

---

## 4. Durability Semantics

Durability is guaranteed at the **fsync boundary**.

Model:

* WAL entries are appended to an OS file
* fsync is performed in configurable batches

Implications:

* entries written after the last fsync may be lost on crash
* coordinator may have responded optimistically

Contract:

* durability is defined **up to the last successful fsync**
* clients and workers must tolerate lost acknowledgements

This is an explicit design choice, not a bug.

---

## 5. Replay Semantics

Replay is:

* deterministic
* sequential
* driven solely by WAL order

Replay **does not** depend on wall-clock time.

Rules:

* timestamps in WAL are treated as metadata
* time-based decisions (e.g. lease expiry) are re-evaluated after replay

This prevents stale liveness or ownership after restart.

---

## 6. Idempotency & Apply Semantics

WAL entries are **not inherently idempotent**.

Examples:

* applying `TaskCreated` twice would duplicate tasks
* applying `LeaseGranted` twice would corrupt attempt counts

Therefore:

* WAL replay must apply each record **exactly once**
* duplicate application is considered a correctness bug

Idempotency is enforced by replay mechanics, not by event logic.

---

## 7. Failure Model

The WAL is designed to tolerate:

* process crashes
* SIGKILL
* machine reboot
* partial / torn writes at the end of the log

The WAL is **not** required to tolerate:

* disk corruption beyond the last record
* Byzantine failures

On restart:

* a partially written final record may be discarded
* all preceding records must be valid and replayable

---

## 8. Compaction & Growth

For v1:

* the WAL is append-only and unbounded
* no snapshotting or truncation

This is intentional to:

* keep implementation simple
* surface replay cost early

Compaction will be addressed in a later iteration.

---

## 9. Concurrency Model

The WAL uses a **single-writer model**:

* one logical writer serializes all WAL appends
* state mutation occurs only after WAL append

This does **not** imply a single-threaded coordinator.

Allowed:

* concurrent request handling
* internal request queues

Not allowed:

* concurrent WAL writers
* concurrent state mutation

Correctness is prioritized over throughput.

---

## 10. Explicit Non-Goals

The WAL does not aim to provide:

* high write throughput
* replication
* cross-node durability
* exactly-once execution semantics

Those are separate concerns.

---

## 11. Open Questions (Intentionally Deferred)

The following are **not decided yet**:

* WAL record format
* record framing and checksums
* acknowledgement timing relative to fsync
* snapshot / compaction strategy

These will be addressed incrementally.

---

## 12. Mental Model Summary

* WAL is truth
* everything else is derived
* ordering is global
* durability is explicit and bounded
* replay is deterministic
* time is not replayed

If the system behaves strangely after a crash,
this document should explain why.
