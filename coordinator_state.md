# Coordinator State & Authority Model

This document defines the **single source of truth** for the scheduler.
It exists to prevent split-brain thinking and accidental hidden state.

If a decision is not represented here, it is **not allowed** in the system.

---

## 1. Design Principle (Non‑Negotiable)

> **Any decision that affects task ownership or task state MUST be serialized through the same WAL.**

There is exactly **one authority**:

* the Coordinator
* backed by an append-only WAL

All in-memory state is either:

* authoritative *because it is directly derived from WAL*, or
* cached *and safe to lose*

---

## 2. Authoritative State (WAL-backed)

Authoritative state is reconstructed **only** by replaying the WAL.
Nothing else is trusted on restart.

### 2.1 Task Record (Logical Model)

```
Task {
  task_id
  payload
  state            // WAITING | LEASED | COMPLETED | FAILED | DEAD
  attempt
  current_lease_id // nullable
}
```

Authoritative because:

* every mutation is driven by a WAL entry
* replay produces the same state deterministically

---

### 2.2 Lease Record (Logical Model)

```
Lease {
  lease_id
  task_id
  worker_id
  expiry_timestamp
}
```

Important:

* leases do NOT exist independently of tasks
* a lease is only valid if it matches the task’s `current_lease_id`

---

## 3. Derived / In‑Memory State (Rebuildable)

Derived state exists only for performance and scheduling convenience.
It MUST be safe to lose entirely on coordinator crash.

### 3.1 Task Indexes

```
waiting_tasks      -> set<task_id>
leased_tasks       -> map<task_id, lease_id>
completed_tasks    -> set<task_id>
```

These are:

* rebuilt during WAL replay
* never written independently

---

### 3.2 Worker Registry (Soft State)

```
WorkerState {
  worker_id
  last_heartbeat
  active_leases
}
```

Rules:

* worker state is **not authoritative**
* it is advisory only
* it influences scheduling decisions but never task truth

On restart:

* registry starts empty
* workers must re-heartbeat

---

## 4. WAL Event Types (The Only Allowed Mutations)

Every externally meaningful change maps to **exactly one WAL entry**.

### 4.1 TaskCreated

```
TaskCreated {
  task_id
  payload
  created_at
}
```

Effect:

* creates task
* state = WAITING
* attempt = 0

---

### 4.2 LeaseGranted

```
LeaseGranted {
  task_id
  lease_id
  worker_id
  expiry_timestamp
}
```

Effect:

* task must be WAITING
* task.state -> LEASED
* task.current_lease_id = lease_id
* attempt += 1

This is the **only way** a task becomes owned.

---

### 4.3 LeaseExtended

```
LeaseExtended {
  lease_id
  new_expiry_timestamp
}
```

Effect:

* lease must exist
* lease must not be expired
* expiry is updated

No state change on task.

---

### 4.4 LeaseExpired (Logical Event)

This may be:

* explicitly written, or
* implicitly derived from time during replay

Effect:

* task.state -> WAITING
* task.current_lease_id = null

Important:

* expiry is a **fact of time**, not worker intent

---

### 4.5 TaskCompleted

```
TaskCompleted {
  task_id
  lease_id
}
```

Preconditions:

* task.state == LEASED
* lease_id matches current_lease_id
* lease not expired

Effect:

* task.state -> COMPLETED
* lease is invalidated

---

### 4.6 TaskCancelled

```
TaskCancelled {
  task_id
  lease_id
}
```

Used when:

* completion arrives with invalid / expired lease

Effect:

* no task state change
* worker is informed of authority loss

This is **not a failure**.

---

### 4.7 TaskFailed

```
TaskFailed {
  task_id
  lease_id
  reason
}
```

Effect:

* task.state -> WAITING or FAILED (based on retry policy)
* lease invalidated

---

## 5. Atomicity Rules (Critical)

### 5.1 Single‑Record Atomicity

Each WAL entry is atomic.
No partial effects.

State transitions happen **after** WAL append succeeds.

---

### 5.2 No Cross‑Entry Transactions

You may NOT:

* update task state
* then write WAL

WAL is always first.

If WAL write fails:

* operation did not happen

---

### 5.3 Scheduling Atomicity

Scheduling a task means:

```
append LeaseGranted
apply state transition
```

No intermediate visible state.

---

## 6. Crash Recovery Semantics

On coordinator restart:

1. Read WAL from beginning
2. Replay entries sequentially
3. Rebuild:

   * tasks
   * leases
   * indexes
4. Expire leases based on current time
5. Accept new requests

No background reconciliation.
No repair jobs.

Replay is truth.

---

## 7. Invariants (Must Always Hold)

* A task has at most one active lease
* A lease always refers to exactly one task
* A task in WAITING has no lease
* A task in LEASED has exactly one valid lease
* COMPLETED tasks never transition again

Violating any invariant is a bug.

---

## 8. Mental Model Summary

* WAL is the system of record
* Time revokes ownership
* Workers are unreliable narrators
* Leases grant permission, not success
* Coordinator never guesses

If something feels like it requires “just one more flag”,
stop and re-check the invariants.
