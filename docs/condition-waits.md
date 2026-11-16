# Condition waits: first increment

Status: Process-exit conditions implemented. Element conditions, request conditions, action expectations, and event cursors remain planned.

The Linux tool `task_wait` waits for a process resource returned by `task_launch`. It runs outside the global screen lock. A concurrent `task_close` can stop the process and end the wait.

```json
{"task_id":"<task id>","resource_id":"<process resource id>","exit_code":0,"timeout_ms":10000}
```

Omit `exit_code` to accept any observed exit code. The default deadline is 10 seconds. The maximum is 10 minutes. Inspection and polling share one deadline. Request cancellation reaches lock acquisition and systemd queries.

The result contains `status`, `elapsed_ms`, and `last_observation`:

- `satisfied`: the process exited and its code matches the expectation.
- `failed`: the exit code differs, or the task or resource was closed.
- `timed_out`: no terminal outcome was observed before the deadline.
- `cancelled`: the request was cancelled. A disconnected client may receive a transport cancellation instead of the result.
- `unsupported`: process state or exit evidence is unavailable.

The last observation includes task state and the resource record when available. A missing service without a retained exit code does not prove a successful exit. Invalid task ownership, an unknown resource ID, or an invalid timeout returns a tool error.

Existing `wait` and `find` inputs remain unchanged. Browser integration is not changed by this increment.

## Next slices

1. Add element presence and absence through short, cancellable observations. Preserve screen-instance identity across observations. A snapshot error must not satisfy absence.
2. Agree on request-event handles with the bare integration. Add event cursors and explicit evidence gaps before request waits.
3. Register action expectations before input dispatch. Return the action ID and matched success or failure evidence together.
4. Test a local form fixture across success, HTTP failure, server exit, timeout, cancellation, and concurrent cleanup.
