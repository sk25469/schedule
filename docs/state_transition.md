# Task State Transitions

This document defines **all legal and illegal state transitions** for a task.

If a transition is not listed here as **ALLOWED**, it is **FORBIDDEN**.
There are no implicit transitions.

This document exists to make incorrect states *unrepresentable*.

---

## 1. Task States (Authoritative)

* `WAITING`    – task is eligible to be leased
* `LEASED`     – task is owned by a worker via a valid lease
* `COMPLETED`  – task finished successfully (terminal)
* `FAILED`     – task permanently failed (terminal)
* `DEAD`       – task administratively stopped (terminal)

Terminal states:

* `COMPLETED`
* `FAILED`
* `DEAD`

Once a task enters a terminal state, **no further transitions are allowed**.

---

## 2. Events

Events are **external or time-based triggers** that may cause a state transition.

* `TaskCreated`
* `LeaseGranted`
* `LeaseExtended`
* `LeaseExpired`
* `TaskCompleted`
* `TaskFailed`
* `TaskCancelled`

Only these events may cause transitions.

---

## 3. State Transition Table

| Current State | Event         | Next State       | Allowed | Notes                                |
| ------------- | ------------- | ---------------- | ------- | ------------------------------------ |
| —             | TaskCreated   | WAITING          | YES     | Initial creation                     |
| WAITING       | LeaseGranted  | LEASED           | YES     | Attempt increments                   |
| WAITING       | TaskCompleted | —                | NO      | Cannot complete without lease        |
| WAITING       | TaskFailed    | —                | NO      | Failure requires lease               |
| WAITING       | LeaseExpired  | —                | NO      | No lease to expire                   |
| LEASED        | LeaseExtended | LEASED           | YES     | Expiry updated only                  |
| LEASED        | TaskCompleted | COMPLETED        | YES     | Lease must be valid                  |
| LEASED        | TaskFailed    | WAITING / FAILED | YES     | Depends on retry policy              |
| LEASED        | LeaseExpired  | WAITING          | YES     | Ownership revoked by time            |
| LEASED        | LeaseGranted  | —                | NO      | Cannot double-lease                  |
| LEASED        | TaskCancelled | LEASED           | YES     | Authority loss only; no state change |
| COMPLETED     | *any*         | —                | NO      | Terminal state                       |
| FAILED        | *any*         | —                | NO      | Terminal state                       |
| DEAD          | *any*         | —                | NO      | Terminal state                       |

---

## 4. Forbidden Transitions (Explicit)

The following transitions are **always illegal**:

* `WAITING → COMPLETED`
* `WAITING → FAILED`
* `LEASED → LEASED` via `LeaseGranted`
* `LEASED → COMPLETED` with expired or mismatched lease
* Any transition *from* a terminal state

Any attempt to perform these must result in:

* WAL append rejection
* protocol error (`REJECTED`)

---

## 5. Cancellation Semantics

`TaskCancelled` is **not** a state transition.

It represents:

* loss of authority by a worker
* rejection of a completion attempt

Effects:

* no task state change
* no retry triggered
* worker receives `CANCELLED`

This event exists solely to communicate outcome to the worker.

---

## 6. Retry Semantics

Retries are evaluated **only** on `TaskFailed`.

Rules:

* retry count increments per attempt
* retry policy decides:

  * transition to `WAITING` (retry)
  * transition to `FAILED` (terminal)

Lease expiry does **not** count as a failure.

---

## 7. Time-Based Transitions

Time affects the system only through:

* `LeaseExpired`

Rules:

* time may revoke ownership
* time never completes tasks
* time never fails tasks

Time only removes permission, never grants success.

---

## 8. Invariants (Re-stated)

At all times:

* A task in `WAITING` has no lease
* A task in `LEASED` has exactly one valid lease
* A task in a terminal state has no lease
* A lease always refers to a `LEASED` task

Violating any invariant is a correctness bug.

---

## 9. Mental Model Summary

* States change only via events
* Events are serialized through WAL
* Leases gate authority, not execution
* Cancellation is not a transition
* Time revokes ownership, nothing else

If you feel tempted to add a new state or shortcut a transition,
revisit this table first.
