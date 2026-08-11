# Runtime Modes

Governor supports three runtime modes. They trade off startup work, steady-state CPU use, and how aggressively the server watches for changes.

The estimates below are deliberately conservative and approximate. Actual usage depends on repository size, number of files, enabled integrations, and host disk speed.

## Modes

| Mode | Background work | Expected resource use | Recommended machine | Best for |
|------|-----------------|-----------------------|----------------------|----------|
| `light` | No file watchers, no periodic graph rebuilds, and no startup graph warmup. Graph work happens on demand. | Lowest steady-state CPU. Lowest memory pressure. Fastest startup. | 2 vCPU, 4-8 GB RAM, SSD. | Laptops and small development boxes, including lower-power local machines. |
| `balanced` | No continuous watchers. One-time graph warmup on startup so the first tool call is faster. | Moderate startup CPU. Low steady-state CPU after warmup. Moderate memory. | 4 vCPU, 8-16 GB RAM, SSD. | Day-to-day development where you want faster graph-backed tools without continuous watching. |
| `full` | Startup graph warmup, live filesystem watchers, docgov watcher, and periodic graph rebuilds. | Highest and most sustained CPU use. Highest memory pressure. | 8+ vCPU, 16-32 GB RAM, fast SSD. | CI-style validation, active multi-agent sessions, or large repos where live freshness matters. |

## Defaults

- The repository sample config uses `light` mode by default.
- `--mode balanced` is the middle ground when you want faster graph-backed tools but do not want continuous watchers.
- `--mode full` or `--full-mode` turns on the old always-watching behavior.

## What Each Mode Changes

- `light`:
  - Skips the graph rebuild loop.
  - Skips the filesystem watcher.
  - Skips the docgov watcher.
  - Leaves expensive graph work until a request actually needs it.
- `balanced`:
  - Builds the graph once at startup.
  - Does not start continuous watchers.
  - Avoids periodic rebuild churn.
- `full`:
  - Builds the graph at startup.
  - Starts the call graph watcher.
  - Starts the docgov watcher.
  - Runs periodic rebuilds in the background.

## Recommendation For This Machine

On a laptop or desktop that is already carrying multiple agents and editors, `light` is the safest default. It keeps the server available without forcing constant rebuild churn in the background. Use `balanced` only when you want faster graph-backed responses and can afford the startup cost. Use `full` only when you need live file tracking and are willing to pay for it.
