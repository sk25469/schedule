# WAL Record Payloads (Authoritative)

This document defines the **payload semantics** for all remaining WAL record types.

Each record payload is the **minimum self-sufficient representation** required to:

* validate correctness during replay
* preserve invariants
* avoid implicit or inferred state

All records defined here follow the rule:

> **A WAL record must be applicable in isolation, assuming only prior WAL replay.**

---

## 1. TaskCreated

### Semantic Meaning

* a new task is introduced into the system
* the task did not exist before this record
* task enters the `WAITING` state
* attempt counter is initialized

### Required Payload

```
TaskCreated {
  task_id
  payload
  execution_window
  retry_policy
  request_id?
  created_at?
}
```

### Why These Fields

* `task_id`

  * uniquely identifies the task for all future records

* `payload`

  * opaque task input
  * required for execution after replay

* `execution_window`

  * defines max ownership duration per attempt
  * required for correct lease validation

* `retry_policy`

  * defines retry vs terminal failure behavior

* `request_id` (optional)

  * enables idempotent task submission

* `created_at` (optional)

  * metadata only; not used for correctness

### Invariants Checked on Apply

* task_id must not already exist

---

## 2. LeaseGranted

### Semantic Meaning

* ownership of a task is granted to a worker
* a new execution attempt begins
* task transitions from `WAITING` to `LEASED`

### Required Payload

```
LeaseGranted {
  task_id
  lease_id
  worker_id
  attempt
  lease_expiry
  granted_at?
}
```

### Why These Fields

* `task_id`

  * identifies the task being leased

* `lease_id`

  * uniquely identifies ownership
  * used to validate authority

* `worker_id`

  * identifies the owner of the lease

* `attempt`

  * explicit execution attempt number
  * required for retry correctness

* `lease_expiry`

  * absolute timestamp
  * enables time-based revocation without replay-time clocks

* `granted_at` (optional)

  * metadata only

### Invariants Checked on Apply

* task must exist
* task must be in `WAITING` state
* attempt must equal previous_attempt + 1

---

## 3. LeaseExtended

### Semantic Meaning

* an existing lease remains authoritative
* ownership duration is extended
* no task state transition occurs

### Required Payload

```
LeaseExtended {
  lease_id
  new_lease_expiry
}
```

### Why These Fields

* `lease_id`

  * uniquely identifies the lease being extended
  * prevents accidental extension of wrong ownership

* `new_lease_expiry`

  * absolute timestamp
  * avoids time-dependent replay logic

### Invariants Checked on Apply

* lease must exist
* lease must be currently valid
* new expiry must be > current expiry

---

## 2. TaskCompleted

### Semantic Meaning

* a task attempt finished successfully
* ownership is relinquished
* task becomes terminal

### Required Payload

```
TaskCompleted {
  task_id
  lease_id
}
```

### Why These Fields

* `task_id`

  * identifies the task transitioning

* `lease_id`

  * proves authority at completion time
  * enables rejection of stale completions

### Invariants Checked on Apply

* task must exist
* task must be in `LEASED` state
* lease_id must match current lease
* lease must not be expired

---

## 3. TaskFailed

### Semantic Meaning

* a task attempt failed during execution
* retry policy is evaluated

### Required Payload

```
TaskFailed {
  task_id
  lease_id
  failure_reason
}
```

### Why These Fields

* `task_id`

  * identifies the affected task

* `lease_id`

  * proves authority of the failing worker

* `failure_reason`

  * opaque string / enum
  * used for debugging and observability

### Invariants Checked on Apply

* task must exist
* task must be in `LEASED` state
* lease_id must match current lease

### State Effects

* if retries remain → task transitions to `WAITING`
* if retries exhausted → task transitions to `FAILED`

---

## 4. TaskCancelled

### Semantic Meaning

* worker attempted an action without valid authority
* result is explicitly rejected

### Required Payload

```
TaskCancelled {
  task_id
  lease_id
}
```

### Why This Record Exists

* makes authority loss explicit
* preserves coordination history
* avoids silent discarding of results

### Important Notes

* this record does **not** change task state
* it exists purely for protocol clarity

---

## 5. LeaseExpired (Logical)

### Semantic Meaning

* ownership is revoked due to time
* task becomes unowned

### Payload

This record may be:

#### Implicit (Preferred for v1)

* derived during replay or decision-time
* not physically written to WAL

#### Explicit (Optional)

```
LeaseExpired {
  task_id
  lease_id
}
```

### Notes

* expiry is a fact of time, not intent
* time may revoke ownership, never grant it

---

## 6. TaskDead (Administrative, Optional)

### Semantic Meaning

* task is forcibly terminated
* no further retries

### Required Payload

```
TaskDead {
  task_id
  reason
}
```

### Notes

* terminal transition
* bypasses retry logic

---

## 7. Cross-Record Invariants (Global)

At all times:

* a task has at most one active lease
* lease_id uniquely identifies ownership
* only `LeaseGranted` creates ownership
* only `TaskCompleted` / `TaskFailed` end attempts
* time revokes ownership, not workers

---

## 8. Mental Model Summary

* records encode facts, not intent
* payloads are minimal but sufficient
* replay never guesses
* invariants are enforced, not inferred

If a future feature cannot be expressed using these payloads,
it must be redesigned.
