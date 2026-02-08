# Coordinator Core Loop & Architecture

This document describes the **execution model of the Coordinator**.
It defines how requests flow through the system, how decisions are made, how WAL entries are chosen and applied, how time revokes ownership, and how the system recovers from crashes.

This is the **heart of the scheduler**. If this loop is wrong, nothing else matters.

---

## 1. High-Level Architecture

The system consists of a **single logical Coordinator** backed by a **single authoritative WAL**.

There are no independent services with authority. All components below are **internal modules** within the Coordinator process.

```
                ┌─────────────────────────┐
                │        Coordinator      │
                │                         │
                │  ┌───────────────┐      │
submit_task ───▶│  │ TaskManager   │      │
heartbeat   ───▶│  └───────────────┘      │
complete    ───▶│  ┌───────────────┐      │
fail        ───▶│  │ LeaseManager  │      │
                │  └───────────────┘      │
                │  ┌───────────────┐      │
                │  │ WorkerRegistry│      │
                │  └───────────────┘      │
                │           │             │
                │        WAL append        │
                │           │             │
                │     Apply WAL entry      │
                └─────────────────────────┘
```

### Core Principles

* The **WAL is the only source of truth**
* All authoritative decisions are serialized into the WAL
* In-memory state is derived exclusively from WAL replay
* Time may revoke ownership but never grant success

---

## 2. Internal Modules (Roles & Boundaries)

### 2.1 TaskManager

Responsibilities:

* validate task submissions
* construct `TaskCreated` WAL events
* apply task-related WAL events to in-memory state

Restrictions:

* must never mutate task state directly
* must never infer state from databases

---

### 2.2 LeaseManager

Responsibilities:

* decide whether a task should be leased
* choose a worker based on advisory worker state
* determine lease duration and expiry
* construct `LeaseGranted` and `LeaseExtended` WAL events

Restrictions:

* cannot grant leases without WAL
* cannot track leases outside WAL-backed state

LeaseManager is **pure decision logic**, not an authority.

---

### 2.3 WorkerRegistry

Responsibilities:

* track last heartbeat time per worker
* track active lease count per worker
* expose advisory capacity signals

Properties:

* soft state only
* safe to lose on crash
* rebuilt lazily from worker heartbeats

WorkerRegistry never affects correctness, only scheduling quality.

---

## 3. Request Ingress Flow

All external requests follow the same pattern:

```
request → validate → choose WAL event → append WAL → apply event → respond
```

There are **no shortcuts**.

### 3.1 Task Submission

1. Client sends `submit_task(payload)`
2. Coordinator validates request
3. TaskManager constructs `TaskCreated`
4. WAL append is attempted
5. On success, event is applied to in-memory state
6. Task enters `WAITING`

If WAL append fails, the request fails.

---

### 3.2 Lease Request (Worker Pull)

1. Worker requests a task lease
2. LeaseManager evaluates:

   * available `WAITING` tasks
   * advisory worker capacity
3. If schedulable, construct `LeaseGranted`
4. Append to WAL
5. Apply event → task becomes `LEASED`
6. Respond to worker with lease

If no task is schedulable, respond empty.

---

### 3.3 Heartbeat

1. Worker sends heartbeat for `(task_id, lease_id)`
2. Coordinator validates lease existence and expiry
3. If valid, LeaseManager constructs `LeaseExtended`
4. Append to WAL
5. Apply event → expiry updated

Expired leases are never resurrected.

---

### 3.4 Completion / Failure

1. Worker sends `complete` or `fail` with `(task_id, lease_id)`
2. Coordinator validates:

   * task is `LEASED`
   * lease matches current lease
   * lease is not expired
3. If valid:

   * append `TaskCompleted` or `TaskFailed`
4. If invalid:

   * append `TaskCancelled` (logical event)
5. Apply event and respond accordingly

---

## 4. WAL Event Selection Rules

For every request, **exactly one** of the following must happen:

* one WAL entry is appended
* or the request is rejected

There are no multi-entry transactions.

Rules:

* WAL append always happens *before* state mutation
* if WAL append fails, the operation did not occur
* idempotency is enforced by validating WAL-derived state

---

## 5. Applying WAL Entries

Applying a WAL entry means:

1. Mutating in-memory task / lease state
2. Updating derived indexes
3. Updating metrics

Application is:

* deterministic
* sequential
* replayable

Applying the same WAL twice must produce the same state.

---

## 6. Time-Based Lease Expiry

Time is handled **inside the coordinator**, not by workers.

### Expiry Check Model

* Leases have an absolute expiry timestamp
* Expiry is checked:

  * before granting new leases
  * before accepting heartbeats
  * before accepting completion
  * periodically in a background tick

When a lease is expired:

* a `LeaseExpired` transition is logically applied
* task returns to `WAITING`

Expiry may be:

* implicit (derived during checks)
* or explicitly written to WAL (implementation choice)

Time never causes task completion or failure.

---

## 7. Coordinator Core Loop (Pseudo-Code)

```
loop:
  handle incoming requests:
    validate
    choose WAL event
    append WAL
    apply event
    respond

  periodically:
    scan for expired leases
    revoke ownership

  on crash:
    stop immediately
```

No background reconciliation beyond WAL replay.

---

## 8. Crash & Restart Semantics

On restart:

1. Load WAL from disk
2. Replay entries sequentially
3. Rebuild in-memory state
4. Expire leases based on current time
5. Accept new requests

Assumptions:

* WAL is append-only
* WAL order defines history
* No other state is trusted

Recovery correctness depends **only** on WAL integrity.

---

## 9. Invariants Re-Enforced

At all times:

* WAL defines truth
* Only one valid lease per task
* Time revokes ownership
* Workers never decide outcomes
* Coordinator never guesses

If any new feature violates these invariants, it must be redesigned.

---

## 10. Mental Model Summary

* Coordinator = state machine
* WAL = history
* Managers = decision helpers
* Time = revocation mechanism
* Workers = unreliable executors

If this loop is sound, the scheduler is sound.
