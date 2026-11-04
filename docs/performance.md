# VM performance history

Collect VM CPU and memory history for sizing. Collection is opt-in,
read-only, and stored apart from captures.

```sh
vsfleet capture perf --window 7d        # reads vCenter
vsfleet get perfcollections
vsfleet top vm                          # offline
vsfleet export rvtools --file e.xlsx    # adds the vsfleetPerformance sheet
```

vsfleet uses the finest vCenter statistics interval that covers the whole
`--window`; `--interval` picks another. Samples are interval averages,
typically 5 minutes to 2 hours, so a peak can understate a short spike.

| Metric | Unit | Statistics level |
| --- | --- | --- |
| `cpu.usage` | percent | 1 |
| `cpu.ready` | percent of time one vCPU waited | 1 |
| `mem.active` | MiB | 2 |
| `mem.consumed`, `mem.balloon` | MiB | 1 |
| `mem.swapped` | MiB | 2 |
| `mem.swapinRate` | KBps | 1 |
| `mem.usage` | percent of configured memory | 1 |

vCenter keeps level 1 by default, so `mem.active` and `mem.swapped` are
usually missing. vsfleet then reads active memory from `mem.usage` and marks
it with `*`. If neither is kept, raise the interval to level 2 in **vCenter
› Configure › General › Statistics** and wait for history to build; vsfleet
never changes this itself. Storage I/O is not collected.

## Bounds and impact

`--window`, `--max-samples`, `--max-vms`, `--max-requests` and
`--max-runtime` limit a collection. Reaching one marks it `partial` and lists
the VMs not sampled. Each collection prints the requests it used and its
runtime, and stores summaries rather than raw samples.

## Unknown is never zero

Missing samples, such as powered-off time or expired history, never count as
zero. A statistic is `unavailable` when its counter cannot be read, and
`insufficient-data` with fewer than 12 samples or under half the window.
P95 needs 50 samples.

## Sizing signal

Signals describe the window; they are not resize instructions.

| Signal | Meaning |
| --- | --- |
| `contention-observed` | CPU ready reached 5% per vCPU, or memory was ballooned or swapped. Outranks the others: a starved VM looks idle. |
| `peaks-observed` | Usually under 30%, but a peak reached 60% or more |
| `in-use` | Usage is not low |
| `sustained-low` | Peaks stayed under 30% of CPU and memory for the whole window |
| `cpu-only` | CPU is known; memory could not be read, so it is unknown, not low |
| `insufficient-data` | Too few samples, including VMs powered off or not sampled |
| `unavailable` | CPU usage could not be read |

## In reports

`describe capture` shows, for each context, the collection whose window
covers the capture, or the nearest one and whether it falls before or after.
VMs the collection did not sample are counted as `not sampled`. The
`vsfleetPerformance` sheet has one row per VM per metric with the window,
average, peak, P95, status and signal; a context without a collection gets
one `not collected` row. `capture prune` removes old collections but keeps
each context's newest.

## Limits

On vCenter 8.0.3 at level 1, `mem.usage` matched active memory within 0.01
points. Another version that reports consumed memory there would make VMs
look busier, never idler. The `sustained-low`, `peaks-observed` and `in-use`
thresholds have not been checked against a real vCenter at level 2.

<span id="validation-status"></span>
