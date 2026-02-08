# Worker Protocol & Contract

This document defines the **strict contract** between a Worker and the Coordinator.
It exists to prevent ambiguous retries, hidden assumptions, and accidental side effects.

If a worker violates this contract, the scheduler **makes no guarantees**.

---

## 1. Design Principle

> **Workers execute tasks, but never decide task truth.**

Only the Coordinator decides:

* task state
* task ownership
* whether a result is accepted

Workers are replaceable, fallible, and not trusted with authority.

---

## 2. Worker Lifecycle Overview

A worker repeatedly performs the following loop:

1. Request a task lease
2. Execute task payload
3. Heartbeat while executing
4. Report outcome
5. Accept coordinator response

At every step, the worker assumes:

* responses may be delayed
* requests may be retried
* ownership may be lost silently

---

## 3. Coordinator Responses (Authoritative)

When a worker reports completion or failure, it receives **exactly one** of the following responses.

These responses are **final for that attempt**.

---

### 3.1 `COMMITTED`

**Meaning**:

* the worker held a valid lease
* the task result was accepted
* the task state is now terminal (`COMPLETED` or `FAILED`)

**Worker obligations**:

* treat the task as finished
* release all local resources
* do NOT retry
* do NOT re-report

This is the *only* response that means success at the scheduler level.

---

### 3.2 `CANCELLED`

**Meaning**:

* the worker executed the task
* but no longer held valid authority
* the result was discarded

This occurs when:

* lease expired
* lease was superseded by another attempt
* coordinator recovered and invalidated old leases

**Important**:

* `CANCELLED` is **not a failure**
* it does **not** imply a retry
* it does **not** imply the task was incorrect

**Worker obligations**:

* stop all work related to this task
* discard results
* do NOT retry
* do NOT emit side effects after receiving this response

`CANCELLED` means *loss of permission*, not execution error.

---

### 3.3 `REJECTED`

**Meaning**:

* the request itself was invalid

Examples:

* unknown task ID
* invalid lease ID
* malformed request
* duplicate completion for same lease

**Worker obligations**:

* treat as a fatal protocol error
* log and surface the error
* do NOT retry automatically

`REJECTED` indicates a bug or misuse of the protocol.

---

## 4. What a Worker Must Never Do After `CANCELLED`

After receiving `CANCELLED`, a worker must **never**:

* retry the task
* re-submit results
* assume partial success
* emit external side effects
* update external state based on execution

Why:

* another worker may have already completed the task
* retries amplify duplicates
* external systems may diverge

After `CANCELLED`, the worker must behave as if:

> *“This execution never had authority.”*

---

## 5. Heartbeat Rules

* heartbeats are sent **only while executing**
* heartbeats are advisory, not authoritative
* missing heartbeats imply loss of lease
* expired leases are never resurrected

Heartbeat failure is handled by the coordinator, not the worker.

---

## 6. Retry Rules (Worker Boundary)

### 6.1 What the Worker May Retry

Workers may retry **transport-level failures**:

* request timeout
* connection reset
* transient network errors

These retries must be:

* bounded
* idempotent

---

### 6.2 What the Worker Must Never Retry

Workers must **never retry** based on coordinator responses:

* `COMMITTED`
* `CANCELLED`
* `REJECTED`

All three are terminal for the attempt.

---

### 6.3 Execution Failures

If execution fails locally (panic, error, timeout):

* worker reports `fail(task_id, lease_id, reason)`
* coordinator decides retry policy
* worker does not self-schedule retries

Retries are a **scheduler concern**, not a worker concern.

---

## 7. Idempotency Requirements

Workers must assume:

* all RPCs may be duplicated
* responses may be duplicated

Therefore:

* heartbeats must be idempotent
* completion reports must be idempotent
* failure reports must be idempotent

Correctness depends on coordinator validation, not worker cleverness.

---

## 8. Mental Model Summary

* Workers do work, not truth
* Authority flows from leases
* Time revokes ownership
* Cancellation is not failure
* Retrying without permission is a bug

If a worker cannot follow this contract,
then the scheduler cannot make correctness guarantees.
