# Product Requirements Document

## Distributed Task Scheduler (Learning-Oriented)

---

## 1. Purpose

Build a **minimal, distributed task scheduler** to deeply understand:

* task ownership and leasing
* failure and recovery semantics
* time as a first-class constraint
* coordination without pretending reliability
* why “just use Kafka / queue” is often hand-wavy

This system is **not** intended for production use.
It is intended to **surface tradeoffs explicitly**.

---

## 2. Non-Goals

This project explicitly does **not** aim to:

* compete with Airflow / Temporal / Celery / etc.
* provide exactly-once execution
* support DAGs or workflows
* provide priorities or fairness guarantees (initially)
* hide complexity behind “simple APIs”
* optimize for throughput or latency
* offer a UI

If something feels painful or verbose, that is likely **by design**.

---

## 3. Core Abstractions

### 3.1 Task

A **Task** is an immutable intent with mutable execution state.

Minimal fields:

* `task_id`
* `payload` (opaque to scheduler)
* `created_at`
* `state`
* `attempt`
* `lease_id` (if leased)
* `lease_expiry`

The scheduler **does not** interpret task semantics.

---

### 3.2 Task States (Authoritative)

A task must be in **exactly one** state at any point in time.

States:

* `WAITING` – eligible to be leased
* `LEASED` – assigned to a worker, not yet completed
* `COMPLETED` – finished successfully
* `FAILED` – permanently failed (after retries)
* `DEAD` – administratively stopped / poison pill

There is **no implicit state** like `RUNNING`.
Those are **derived interpretations**, not stored truth.

---

### 3.3 Lease

A **Lease** represents temporary ownership of a task.

Properties:

* exclusive
* time-bound
* renewable
* revocable only via expiry (no force-kill)

A task **without a valid lease is unowned**.

---

## 4. System Components

### 4.1 Coordinator

Single logical authority (initially).

Responsibilities:

* accept task submissions
* assign leases
* enforce lease expiration
* transition task states
* persist all state transitions

Coordinator characteristics:

* stateful
* crash-recoverable

Internal constraints:

* append-only write-ahead log (WAL)
* in-memory state derived from WAL
* crash recovery via WAL replay

---

### 4.2 Workers

Stateless executors.

Responsibilities:

* poll coordinator for tasks
* execute task payload
* heartbeat lease
* report success or failure

Workers are assumed to:

* crash
* stall
* disappear
* fail silently

This is expected behavior.

---

## 5. APIs (Conceptual)

### 5.1 Submit Task

```
submit(task_payload) -> task_id
```

Guarantees:

* task is durably recorded before acknowledgment

---

### 5.2 Lease Task

```
lease(worker_id) -> (task_id, lease_expiry) | empty
```

Guarantees:

* at most one active lease per task
* lease duration is fixed and explicit

---

### 5.3 Heartbeat

```
heartbeat(task_id, lease_id) -> ok | rejected
```

Guarantees:

* only valid leases can be renewed
* expired leases are never resurrected

---

### 5.4 Complete Task

```
complete(task_id, lease_id, result)
```

Guarantees:

* completion is idempotent
* late completions after lease expiry are rejected

---

### 5.5 Fail Task

```
fail(task_id, lease_id, error)
```

Guarantees:

* attempt counter increments
* retry policy applied deterministically

---

## 6. Failure Semantics (Core of the System)

### 6.1 Worker Crash

* lease eventually expires
* task returns to `WAITING`
* task may be executed again

Duplicate execution is **acceptable**.

---

### 6.2 Coordinator Crash

* WAL replay restores authoritative state
* in-flight leases without WAL evidence are discarded

Coordinator recovery time must be observable.

---

### 6.3 Network Partition

* coordinator is the source of truth
* workers with expired leases are ignored
* tasks are eventually re-leased

No attempt is made to mask partitions.

---

### 6.4 Lost Responses

* completion without acknowledgment ≠ completion
* idempotency is required at task consumer level

Scheduler does not hide this reality.

---

## 7. Consistency Model

* At-least-once execution
* No exactly-once guarantees
* Strong consistency on task state transitions (via coordinator)
* Time-based guarantees are best-effort

Clocks are assumed to drift.

---

## 8. Observability (Mandatory)

The system must expose:

* number of leased tasks
* lease expirations
* duplicate executions
* retry counts
* coordinator restart count
* WAL replay duration

If it cannot be observed, it is not understood.

---

## 9. Configuration Knobs

Explicit and visible configuration:

* lease duration
* max retry count
* retry backoff strategy (fixed initially)
* heartbeat interval

Defaults must be conservative.

---

## 10. Security & Multi-Tenancy

Out of scope.

This is a **single-tenant learning system**.

---

## 11. Success Criteria (Learning-Oriented)

The project is successful if:

* you can explain exactly why a task ran twice
* you can crash the coordinator and recover safely
* you can reason about every state transition
* adding features feels uncomfortable
* “just use Kafka” no longer sounds sufficient

---

## 12. Open Questions

These are intentionally unresolved:

* coordinator replication
* task sharding
* long-running task handling
* soft vs hard leases
* poison-pill thresholds

These represent future learning vectors, not v1 blockers.

---

### Final Note

If the system ever feels:

* clean
* obvious
* easy

Then something important is being ignored.

Distributed schedulers are **state machines under time pressure**.
This document defines the minimum surface where reality applies force.
