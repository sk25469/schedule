# End-to-End Unhappy Path (Lease Expiry & Duplicate Execution)

This document walks through a **realistic failure scenario** where:

* a task lease expires mid-execution
* the task is reassigned
* a late completion is rejected

The goal is to validate:

* lease authority semantics
* attempt correctness
* cancellation handling
* WAL ordering under failure

This scenario is *expected behavior*, not an error case.

---

## Actors

* **Client** – submits the task
* **Coordinator** – single authority
* **Worker W1** – slow / stalled worker
* **Worker W2** – healthy worker

---

## Initial Conditions

* Coordinator is running
* WAL is empty
* Two workers (W1, W2) are alive

---

## Step 1: Client Submits Task

Same as happy path.

### WAL Record

```
TaskCreated {
  task_id = T1
  payload = P
  execution_window = 5m
  retry_policy = R
}
```

### State After Apply

```
Task T1:
  state = WAITING
  attempt = 0
```

---

## Step 2: Task Leased to Worker W1

### WAL Record

```
LeaseGranted {
  task_id = T1
  lease_id = L1
  worker_id = W1
  attempt = 1
  lease_expiry = t0 + 30s
}
```

### State After Apply

```
Task T1:
  state = LEASED
  attempt = 1
  current_lease = L1
```

---

## Step 3: Worker W1 Starts Execution

* Worker W1 executes task P
* Heartbeats are sent initially

At some point:

* Worker W1 stalls
* Heartbeats stop

---

## Step 4: Lease Expires

* Current time > `lease_expiry`
* Coordinator detects expiry during scheduling loop

### Logical Effect

* Lease L1 is considered expired
* Ownership is revoked
* Task transitions back to `WAITING`

(No explicit WAL record required for v1.)

### State After Expiry

```
Task T1:
  state = WAITING
  attempt = 1
  current_lease = null
```

---

## Step 5: Task Reassigned to Worker W2

### WAL Record

```
LeaseGranted {
  task_id = T1
  lease_id = L2
  worker_id = W2
  attempt = 2
  lease_expiry = t1 + 30s
}
```

### State After Apply

```
Task T1:
  state = LEASED
  attempt = 2
  current_lease = L2
```

---

## Step 6: Worker W2 Completes Task

### WAL Record

```
TaskCompleted {
  task_id = T1
  lease_id = L2
}
```

### State After Apply

```
Task T1:
  state = COMPLETED
  attempt = 2
  current_lease = null
```

Coordinator responds to W2 with:

```
COMMITTED
```

---

## Step 7: Late Completion from Worker W1

Worker W1 eventually finishes and sends:

```
complete(task_id=T1, lease_id=L1)
```

### Coordinator Validation

* task exists
* task state == COMPLETED
* lease_id does not match current lease

### WAL Record

```
TaskCancelled {
  task_id = T1
  lease_id = L1
}
```

### Effect

* no state change
* result is discarded

Coordinator responds to W1 with:

```
CANCELLED
```

---

## Replay Safety Check

Replay WAL sequentially:

1. `TaskCreated(T1)` → WAITING
2. `LeaseGranted(T1, L1, attempt=1)` → LEASED
3. `LeaseGranted(T1, L2, attempt=2)` → LEASED
4. `TaskCompleted(T1, L2)` → COMPLETED
5. `TaskCancelled(T1, L1)` → no-op

At all prefixes:

* task state is valid
* at most one lease is authoritative
* late completions are rejected deterministically

---

## Key Observations

* Duplicate execution is expected
* Attempt numbers disambiguate executions
* Lease identity guards authority
* Cancellation is explicit, not implicit
* Replay requires no inference or repair

This validates correctness under failure.
