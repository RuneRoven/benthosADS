## Get started
Get the required modules for benthos.
Compile the example files
Run the example file. in Windows:
./example.exe -c benthos.yaml


### ads
Input for Beckhoffs ads protocol. Supports batch reading and notifications.
Beckhoff recommends not using more than about 500 notifications due to the impact of the controller.
This input only supports symbols and not direct addresses

```yaml
---
input:
  ads:
    targetIP: '192.168.3.70'        # IP address of the PLC
    targetAMS: '5.3.69.134.1.1'     # AMS net ID of the target
    targetPort: 48898               # Port of the target internal gateway
    runtimePort: 801                # Runtime port of PLC system
    hostAMS: '192.168.56.1.1.1'     # Host AMS net ID. Usually the IP address + .1.1
    readType: 'interval'            # Read type, interval or notification
    maxDelay: 100                   # Max delay for sending notifications in ms
    cycleTime: 100                  # Cycle time for notification handler in ms
    intervalTime: 1000              # Interval time for reading in ms
    logLevel: "disabled"            # Log level for ADS connection
    symbols:                        # List of symbols to read from
      - "MAIN.MYBOOL"               # variable in the main program
      - "MAIN.MYTRIGGER:0:10"       # variable in the main program with 0ms max delay and 10ms cycleTime
      - "MAIN.SPEEDOS"
      - ".superDuperInt"            # Global variable
      - ".someStrangeVar"

pipeline:
  processors:
    - bloblang: |
        root = {
          meta("symbol_name"): this,
          "timestamp_ms": (timestamp_unix_nano() / 1000000).floor()
        }

output:
  stdout: {}

logger:
  level: ERROR
  format: logfmt
  add_timestamp: true
```

#### Connection to ADS
When connecting to an ADS device you connect to a router which then routes the traffic to the correct device using the AMS net ID.
There are basically 3 ways for setting up the connection:

1. **TwinCAT Connection Manager**: Use the TwinCAT connection manager locally on the host, scan for the device and add a connection using the correct credentials for the PLC.
2. **Static route on PLC**: Log in to the PLC using the TwinCAT system manager and add a static route from the PLC to the client. This is the preferred way when using benthos on a Kubernetes cluster since you have no good way of installing the connection manager.
3. **Automatic route registration (UDP)**: Use the `routeUsername` and `routePassword` config fields to have the plugin automatically register a route on the PLC before connecting. See the [Route Registration](#route-registration) section below.

#### Docker and Kubernetes

ADS works from inside Docker containers with default bridge networking — **no `host_network`, no port forwarding, and no open ports are needed**. All ADS traffic (requests, responses, and notifications) flows over a single outbound TCP connection to port 48898. The PLC never initiates connections back to the client; it sends all responses and notifications on the same TCP socket the client opened.

The only requirement is that the `hostAMS` value matches a route registered on the PLC. When running in Docker with bridge networking:
- **`routeHostAddress` must be set** to the Docker host's IP on the PLC network (e.g. `192.168.1.50`). This tells the PLC which IP address to associate with the route. If left empty, it auto-detects the container's bridge IP which is not routable from the PLC.
- **`hostAMS` can be set explicitly** to `routeHostAddress` + `.1.1` (e.g. `192.168.1.50.1.1`), or left as `auto` — when route registration is configured with `routeHostAddress`, `auto` will correctly derive the AMS NetID from `routeHostAddress` instead of the container's bridge IP.
- **A route must exist on the PLC** for the `hostAMS` NetID. This can be added manually in TwinCAT System Manager, or automatically via the `routeUsername`/`routePassword` config fields.
- **`hostPort` is optional** (default 10500). It is a logical AMS port used in protocol headers, not a network port. Any value works.

**Option A: Automatic route registration (recommended)**

The plugin registers a route on the PLC automatically via UDP before connecting. No manual PLC configuration needed:

```yaml
input:
  ads:
    targetIP: '192.168.1.100'
    targetAMS: '192.168.1.100.1.1'
    runtimePort: 851
    hostAMS: 'auto'                      # Derives AMS NetID from routeHostAddress
    routeUsername: 'Administrator'        # Triggers automatic route registration
    routePassword: '1'
    routeHostAddress: '192.168.1.50'     # Docker HOST IP (required in bridge networking)
    readType: 'notification'
    symbols:
      - "MAIN.MyVariable"
```

You can also set `hostAMS` explicitly if you prefer:

```yaml
    hostAMS: '192.168.1.50.1.1'          # Explicit: Docker HOST IP + .1.1
    routeHostAddress: '192.168.1.50'     # Must match
```

**Option B: Static route on PLC**

If you prefer not to use automatic registration, add a static route on the PLC via TwinCAT System Manager pointing to the Docker host's IP. Then configure `hostAMS` to match — no `routeUsername`/`routePassword` needed:

```yaml
input:
  ads:
    targetIP: '192.168.1.100'
    targetAMS: '192.168.1.100.1.1'
    runtimePort: 851
    hostAMS: '192.168.1.50.1.1'         # Must match the route on the PLC
    readType: 'notification'
    symbols:
      - "MAIN.MyVariable"
```

**Option C: host_network or macvlan**

When the container has a routable IP on the PLC network, `hostAMS: auto` works without `routeHostAddress`:

```yaml
input:
  ads:
    targetIP: '192.168.1.100'
    targetAMS: '192.168.1.100.1.1'
    runtimePort: 851
    hostAMS: 'auto'                     # Auto-derive from container's real IP
    routeUsername: 'Administrator'       # Optional: auto-register route
    routePassword: '1'
    readType: 'notification'
    symbols:
      - "MAIN.MyVariable"
```

#### Configuration Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| **targetIP** | Yes | — | IP address of the Beckhoff PLC |
| **targetAMS** | Yes | — | AMS net ID of the target |
| **symbols** | Yes | — | List of symbols to read from (see [Symbol Format](#symbol-format) below) |
| **targetPort** | No | `48898` | Port of the target internal gateway |
| **runtimePort** | No | `801` | Runtime port of PLC system, 800–899. TwinCAT 2 uses 800–850 (usually 801), TwinCAT 3 uses 851–899 (usually 851) |
| **hostAMS** | No | `auto` | Host AMS net ID. Usually the IP address + `.1.1`. Must match a route on the PLC. `auto` derives it from `routeHostAddress` if set, otherwise from the outbound connection's local IP |
| **hostPort** | No | `10500` | AMS source port used in protocol headers. This is a logical port, not a network port. Any arbitrary value works |
| **readType** | No | `notification` | Read type for the symbols. `interval` polls at `intervalTime`; `notification` uses PLC push updates (see [Interval vs Notification](#interval-vs-notification)) |
| **maxDelay** | No | `100` | Default max delay for sending notifications in ms. Maximum time after value change before PLC must send the notification |
| **cycleTime** | No | `1000` | Default cycle time for notification handler in ms. How often the PLC scans for changes. Use a low value for triggers that are only true/false for 1 PLC cycle |
| **intervalTime** | No | `1000` | Interval time between reads in ms (only used when `readType` is `interval`) |
| **requestTimeout** | No | `5000` | Timeout for individual ADS requests in ms. Increase for slow PLCs or large symbol tables |
| **transmissionMode** | No | `serverOnChange` | Notification transmission mode. Only applies when `readType` is `notification`. Options: `serverOnChange`, `serverCycle`, `serverOnChange2`, `serverCycle2` (see [Transmission Modes](#transmission-modes)) |
| **logLevel** | No | `disabled` | Log level for ADS connection (`disabled`, `error`, `warn`, `info`, `debug`, `trace`). At `debug`/`trace`, ADS error codes show human-readable descriptions |
| **routeUsername** | No | `""` | Username for automatic UDP route registration on the PLC. If set, a route is registered before connecting (see [Route Registration](#route-registration)) |
| **routePassword** | No | `""` | Password for automatic UDP route registration on the PLC |
| **routeHostAddress** | No | `""` | IP address the PLC associates with the route. Required in Docker bridge networking (set to Docker host's IP). When `hostAMS` is `auto`, the AMS NetID is also derived from this. Auto-detected from outbound connection if empty (only correct with `host_network` or macvlan) |
| **loadSymbols** | No | `false` | Download the full symbol and datatype table from the PLC on connect. Required for struct and array symbols. May cause a brief real-time jitter on the PLC during the initial connection; use with care on large programs |

##### Symbol Format

Symbols are specified in the format `function.variable:maxDelay:cycleTime`:
- `MAIN.MYBOOL` — variable in the main program, uses default maxDelay and cycleTime
- `MAIN.MYTRIGGER:0:10` — variable with 0ms max delay and 10ms cycle time
- `.superDuperInt` — global variable (must start with `.`)

#### Struct and Array Symbols

Two approaches for reading struct members:

**Option A — Subscribe to individual members (dot notation)**

Use dot notation to address any nested field directly. No `loadSymbols` needed. The PLC resolves each path via a single `0xF009` roundtrip on connect and returns the primitive type.

```yaml
symbols:
  - "MAIN.MachineStatus.Motor1.fSpeed"      # → LREAL primitive
  - "MAIN.MachineStatus.Pressure.fValue"    # → LREAL primitive
  - "MAIN.MachineStatus.sMachineName"       # → STRING primitive
```

Each member gets its own change notification — the PLC only sends data when that specific field changes. Best when you need a subset of fields from a large struct or want independent change detection per field.

**Option B — Subscribe to whole struct (requires `loadSymbols: true`)**

Set `loadSymbols: true` to download the full symbol and datatype table on connect. The plugin then decodes the entire struct as a nested JSON object on each notification.

```yaml
loadSymbols: true
symbols:
  - "MAIN.MachineStatus"   # → decoded as nested JSON object
```

The notification fires when any field in the struct changes. Output value is a JSON object with all fields, e.g.:

```json
{
  "Motor1": {"fSpeed": 1087.0, "fTorque": 108.7, "bEnabled": true},
  "Pressure": {"fValue": 7.0, "sUnit": "bar"},
  "sMachineName": "Line1"
}
```

Use this when you need all fields of a struct or the field names are not known in advance. `loadSymbols` downloads the entire symbol table, which may cause a brief real-time jitter on the PLC during the initial connection — use with care on large programs.

#### Transmission Modes

> **Note:** `transmissionMode` only applies when `readType` is `notification`. When using `readType: interval`, the plugin sends plain ADS Read commands to the PLC at each interval — no notification mechanism is involved, and `transmissionMode` is ignored.

The `transmissionMode` field controls how the PLC's internal notification handler sends updates back to the client. The available modes are:

| Mode | Value | Description |
|------|-------|-------------|
| `serverOnChange` | 4 | **(Default)** The PLC scans for changes at the configured `cycleTime` interval and sends a notification only when the value has changed. This is the most efficient mode for most use cases. |
| `serverCycle` | 3 | The PLC sends the current value at every `cycleTime` interval, regardless of whether the value has changed. Useful when you need a constant data stream or heartbeat. |
| `serverOnChange2` | 6 | Enhanced version of `serverOnChange` available on newer TwinCAT 3 firmware. Supports more efficient internal handling on the PLC side. **Automatically falls back** to `serverOnChange` on older PLCs. |
| `serverCycle2` | 5 | Enhanced version of `serverCycle` available on newer TwinCAT 3 firmware. Same behavior as `serverCycle` but with improved internal efficiency. **Automatically falls back** to `serverCycle` on older PLCs. |

**Choosing a mode:**
- Use `serverOnChange` (default) for event-driven data where you only care about changes
- Use `serverCycle` when you need periodic snapshots regardless of changes
- The `2` variants (`serverOnChange2`, `serverCycle2`) can be used safely on any PLC — the plugin automatically detects older PLCs and falls back to the v1 equivalent

```yaml
input:
  ads:
    transmissionMode: 'serverOnChange'   # default, sends only on value change
    # transmissionMode: 'serverCycle'    # sends at every cycle regardless of change
    # transmissionMode: 'serverOnChange2' # enhanced, auto-falls back on older PLCs
    # transmissionMode: 'serverCycle2'    # enhanced cyclic, auto-falls back on older PLCs
```

#### Interval vs Notification

The `interval` and `notification` read types can produce similar-looking results (periodic data), but they work differently under the hood:

- **`interval`**: The client polls the PLC — sends an ADS Read request for each symbol at every `intervalTime` interval. Simple, no PLC notification overhead, and not subject to the ~500-notification limit.
- **`notification` + `serverOnChange`**: The PLC pushes data only when a value changes. Most efficient for event-driven data. Subject to the ~500-notification limit per connection.
- **`notification` + `serverCycle`**: The PLC pushes data at every `cycleTime` interval regardless of changes. Similar result to `interval` but PLC-driven — more precise timing with no request/response overhead per cycle. Subject to the ~500-notification limit.

| Aspect | `interval` | `notification` + `serverOnChange` | `notification` + `serverCycle` |
|--------|-----------|-----------------------------------|-------------------------------|
| Who drives | Client polls | PLC pushes on change | PLC pushes on timer |
| Network per cycle | Request + response | Push only | Push only |
| Sends unchanged values | Yes | No | Yes |
| Timing precision | Subject to network latency | PLC real-time task | PLC real-time task |
| PLC notification limit | No limit | ~500 max | ~500 max |
| Best for | Large symbol lists, simple setup | Event-driven data (most use cases) | Precise periodic sampling |

#### Explanation of cycleTime and maxDelay

**cycleTime** controls how often the PLC checks the variable:
- `serverCycle` mode: PLC sends a notification every `cycleTime` ms regardless of value change
- `serverOnChange` mode: PLC checks the value every `cycleTime` ms and sends a notification only if it changed

**maxDelay** controls how long the PLC can buffer notifications before sending:
- The PLC collects notification events and sends them in a batch when `maxDelay` ms expires
- This is a network optimization: fewer packets, multiple notifications bundled in one AMS packet

**Practical example** — `cycleTime: 10`, `maxDelay: 100`, mode `serverOnChange`:
1. PLC checks variable every 10ms
2. If value changed, queues a notification
3. Sends queued notifications at most every 100ms (batched)

**Edge cases:**
- `maxDelay: 0` — send immediately, no batching
- `cycleTime: 0` — check as fast as the PLC task cycle allows
- `maxDelay` < `cycleTime` — effectively no batching (fires before next check)

Think of it as:
- **cycleTime** = polling interval (sensor sampling rate)
- **maxDelay** = delivery batch window (network efficiency)

**Important:** If a variable changes faster than `cycleTime`, intermediate values are missed:

```
cycleTime = 1000ms, mode = serverOnChange

Time:    0ms    200ms   400ms   600ms   800ms   1000ms
Value:   5  →   10  →   3   →   7   →   2   →   8
PLC checks:  ↑                                    ↑
Notifies: 5                                       8  (missed 10,3,7,2)
```

The PLC only samples at `cycleTime` intervals. Between checks, it is blind — this is not a continuous event stream.

For fast-changing values, set `cycleTime` close to the PLC task cycle time (typically 1–10ms). Even at minimum `cycleTime`, there is no guarantee of capturing every value — if a variable changes twice within one PLC scan cycle, the intermediate value is lost. ADS notifications are polling with push delivery, not event capture.

#### Notification Behavior

##### First batch completeness

When `readType: notification`, TwinCAT sends an initial sample for every subscribed symbol immediately on registration. The plugin waits for these initial samples before returning from `Connect`, so the **first `ReadBatch` always returns a complete batch** containing one message per successfully registered symbol. No separate read or warm-up period is needed to get the current state of all symbols.

##### Partial registration failures

If a symbol fails to register (unknown name, PLC-side ADS error), the plugin:
- Logs an **error** identifying the symbol and reason
- Continues with the remaining symbols — data flows for all successfully registered symbols
- Does **not** trigger a reconnect for partial failures; only a full failure (zero symbols registered) forces a reconnect

A misconfigured symbol name is surfaced immediately in logs without blocking data from the other symbols.

##### Interval read — empty batches during reconnect

When `readType: interval`, the first one or two batches after a reconnect may be **empty or partial** while the go-ads library re-resolves symbol handles. This is normal — Benthos retries the next poll and subsequent batches are complete.

#### Route Registration

The plugin can automatically register a route on the PLC using the Beckhoff UDP route protocol (port 48899). This removes the need to manually add routes in the TwinCAT System Manager.

**Activation:** both `routeUsername` and `routePassword` must be set.

**How it works:**
1. The TCP connection to port 48898 is established
2. The plugin probes the PLC with a lightweight ADS command to check if a route already exists
3. If the probe succeeds, route registration is skipped (route already present)
4. If the probe fails, the plugin sends a UDP registration packet to port 48899: "Associate AMS NetID X with IP address Y"
5. After registration the TCP connection is re-established (some PLCs close connections from previously-unknown NetIDs)
6. On reconnect after a network loss, the same probe-first logic runs automatically

**Parameters:**
- `routeUsername` / `routePassword`: PLC administrator credentials. Same as used in TwinCAT System Manager to add routes
- `routeHostAddress`: The IP address the PLC associates with this client. In Docker with bridge networking, this must be set to the Docker host's IP on the PLC network. When `hostAMS` is `auto`, the AMS NetID is derived from this address. Auto-detected if empty (only correct with `host_network` or macvlan)

**Network requirements:**
- UDP port 48899 must be reachable on the PLC from the client (for route registration)
- TCP port 48898 must be reachable on the PLC from the client (outbound — works through any NAT)

#### Reconnection

The plugin automatically reconnects when the TCP connection is lost (e.g. network cable unplugged, PLC restart). Aggressive TCP keepalive probes detect dead connections within ~13 seconds. On reconnect, the plugin:
1. Re-establishes the TCP connection (retries indefinitely with 5s interval)
2. Reloads the symbol table from the PLC
3. Re-subscribes all notification handles

No manual intervention is needed.

#### Output

Each symbol produces a single message with the payload being the string-encoded value. The following metadata fields are set on each message:

| Metadata key | Description |
|---|---|
| `symbol_name` | Sanitized symbol name (dots/special chars replaced with `_`) |
| `data_type` | PLC data type as reported by the symbol table (e.g. `BOOL`, `INT`, `ST_MyStruct`) |
| `base_type` | Resolved primitive base type for aliases and enums (e.g. `DINT` for an enum backed by DINT) |
| `data_size` | Symbol byte length as reported by the PLC |

`data_type`, `base_type`, and `data_size` are populated lazily on first read and absent if symbol resolution fails. Use `meta("symbol_name")` in a Bloblang processor to route or label messages.

## Testing

Tested and verified:
#### CX7000, TwinCAT 3
- Notifications from Docker container with bridge networking (no host_network)
- Automatic UDP route registration from Docker bridge networking
- Static route with explicit hostAMS (no route registration)
- Reconnection after network loss with automatic notification re-subscribe
- Sum/batch commands for read, add notification, and delete notification

#### CX1020, TwinCAT 2
- Read batches, Add notifications, different cycle times and max delay
- Different datatypes, INT, INT16, UINT, DINT, BOOL, STRUCT, and more
- Automatic fallback from sum commands to individual calls
- Automatic fallback from v2 transmission modes to v1
- Reconnection after network loss with automatic notification re-subscribe
