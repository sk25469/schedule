# WAL Record Types

This document enumerates **all WAL record types** that exist in the system.

Each record type represents a **conceptual fact** that advances authoritative state.
No record represents intent, heuristics, or advisory signals.

If an action cannot be expressed using one of these record types, it is **not allowed** in the system.

This document intentionally does **not** define record payload fields yet.
It only defines **what kinds of facts may exist**.

---

## 1. Task Lifecycle Records

These records define the lifecycle of a task.

### 1.1 `TaskCreated`

Represents the creation of a new task.

Semantic meaning:

* task did not exist before this record
* task now exists in `WAITING` state
* attempt counter is initialized

This is the **only** record that may create a task.

---

### 1.2 `TaskCompleted`

Represents successful completion of a task attempt.

Semantic meaning:

* task must currently be `LEASED`
* lease must be valid
* task transitions to `COMPLETED`
* task becomes terminal

No further records may apply to the task after this.

---

### 1.3 `TaskFailed`

Represents execution failure of a task attempt.

Semantic meaning:

* task must currently be `LEASED`
* lease must be valid
* retry policy is evaluated

Possible effects:

* task transitions back to `WAITING` (retry)
* or task transitions to `FAILED` (terminal)

---

### 1.4 `TaskCancelled`

Represents loss of authority by a worker.

Semantic meaning:

* worker attempted to act without valid lease
* task state is unchanged
* result is discarded

This record exists to:

* make cancellation explicit
* preserve coordination history

This is **not** a task state transition.

---

## 2. Lease Lifecycle Records

These records define task ownership and its validity over time.

### 2.1 `LeaseGranted`

Represents granting ownership of a task to a worker.

Semantic meaning:

* task must be in `WAITING`
* a new attempt begins
* task transitions to `LEASED`
* lease becomes authoritative

This is the **only** way a task becomes owned.

---

### 2.2 `LeaseExtended`

Represents extension of an existing lease.

Semantic meaning:

* lease must exist
* lease must still be valid
* ownership duration is extended

No task state change occurs.

---

### 2.3 `LeaseExpired` (Logical)

Represents revocation of ownership due to time.

Semantic meaning:

* lease validity has ended
* task transitions from `LEASED` to `WAITING`

This record may be:

* implicit (derived from time)
* or explicit (written to WAL)

Time may revoke ownership but never grant it.

---

## 3. Administrative / Control Records (Optional)

These records are not required for v1 but are listed for completeness.

### 3.1 `TaskDead`

Represents administrative termination of a task.

Semantic meaning:

* task transitions to `DEAD`
* task becomes terminal

No retries occur.

---

## 4. Explicit Non-Records

The following **must never** appear in the WAL:

* worker heartbeats
* worker liveness updates
* worker capacity signals
* scheduling decisions not resulting in leases
* internal metrics or debugging events

These are advisory and lossy by design.

---

## 5. Record Design Rules

All record types must satisfy:

1. **Conceptual atomicity**

   * one record = one semantic fact

2. **Invariant preservation**

   * applying a record alone must not produce invalid state

3. **Self-validation**

   * record must carry enough information to be checked against current state

4. **Replay determinism**

   * applying records in order must deterministically reconstruct state

If a proposed record violates any rule above, it must be redesigned.

---

## 6. Mental Model Summary

* Records encode facts, not mutations
* Ordering gives sequence, payload gives meaning
* WAL is history, not intent

This list defines the **entire vocabulary of truth** for the scheduler.
