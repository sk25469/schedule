# End-to-End Happy Path (Single Task Execution)

This document walks through **one complete, successful execution** of a task in the system.

The goal is to validate that:

* WAL semantics are sufficient
* invariants hold at every step
* replay after any prefix produces a valid state

This is a *narrative trace*, not an implementation.

---

## Actors

* **Client** – submits the task
* **Coordinator** – single authority
* **Worker W1** – executes the task

---

## Initial Conditions

* Coordinator is running
* WAL is empty
* No tasks exist
* One worker (W1) is alive and heartbeating

---

## Step 1: Client Submits Task

### Client Action

```
submit_task(payload=P, execution_window=5m, retry_policy=R)
```

### Coordinator Processing

1. Validate request
2. Generate:

   * `task_id = T1`
   * `request_id = R1`
3. Construct WAL record:

```
TaskCreated {
  task_id = T1
  payload = P
  execution_window = 5m
  retry_policy = R
  request_id = R1
}
```

4. Append record to WAL
5. Apply record to in-memory state

### State After Apply

```
Task T1:
  state = WAITING
  attempt = 0
  current_lease = null
```

### Coordinator Response

```
submit_task → ACK(task_id = T1)
```

---

## Step 2: Worker Requests Lease

### Worker Action

```
request_lease(worker_id = W1)
```

### Coordinator Processing

1. Scan WAITING tasks → finds T1
2. Decide to schedule on W1
3. Increment attempt → 1
4. Compute lease expiry → now + 30s
5. Generate `lease_id = L1`
6. Construct WAL record:

```
LeaseGranted {
  task_id = T1
  lease_id = L1
  worker_id = W1
  attempt = 1
  lease_expiry = t_exp
}
```

7. Append record to WAL
8. Apply record to in-memory state

### State After Apply

```
Task T1:
  state = LEASED
  attempt = 1
  current_lease = L1

Lease L1:
  worker = W1
  expiry = t_exp
```

### Coordinator Response

```
lease → (task_id=T1, lease_id=L1, payload=P)
```

---

## Step 3: Worker Executes Task

### Worker Action

* Starts executing payload P
* Sends heartbeats periodically

### Coordinator Handling

* Heartbeats update WorkerRegistry (no WAL writes)
* Lease remains valid

---

## Step 4: Worker Completes Task

### Worker Action

```
complete(task_id=T1, lease_id=L1)
```

### Coordinator Processing

1. Validate:

   * task exists
   * state == LEASED
   * lease_id matches
   * lease not expired
2. Construct WAL record:

```
TaskCompleted {
  task_id = T1
  lease_id = L1
}
```

3. Append record to WAL
4. Apply record to in-memory state

### State After Apply

```
Task T1:
  state = COMPLETED
  attempt = 1
  current_lease = null
```

### Coordinator Response

```
complete → COMMITTED
```

---

## Step 5: Final State

* Task T1 is terminal (`COMPLETED`)
* No active leases
* Worker W1 is free

---

## Replay Safety Check

Replay WAL from beginning:

1. `TaskCreated(T1)` → creates WAITING task
2. `LeaseGranted(T1, L1)` → task becomes LEASED
3. `TaskCompleted(T1, L1)` → task becomes COMPLETED

At every prefix:

* state is valid
* invariants hold
* no inference is required

---

## Key Observations

* Every authoritative change is WAL-backed
* Worker liveness never enters WAL
* Lease authority is provable
* Completion is guarded by lease validity
* Replay reproduces exact state

This validates the end-to-end happy path.
