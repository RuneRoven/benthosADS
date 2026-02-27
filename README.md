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
    hostPort: 10500                 # Host port
    readType: 'interval'            # Read type, interval or notification
    maxDelay: 100                   # Max delay for sending notifications in ms
    cycleTime: 100                  # Cycle time for notification handler in ms
    intervalTime: 1000              # Interval time for reading in ms
    requestTimeout: 5000            # Timeout for individual ADS requests in ms
    transmissionMode: 'serverOnChange'  # Notification transmission mode
    upperCase: true                 # Convert symbol names to all uppercase for older PLCS
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
3. **Automatic route registration (UDP)**: Use the `routeUsername` and `routePassword` config fields to have the plugin automatically register a route on the PLC before connecting. See the [Route Registration](#route-registration-docker--kubernetes) section below.

#### Configuration Parameters
- **targetIP**: IP address of the PLC
- **targetAMS**: AMS net ID of the target
- **targetPort**: Port of the target internal gateway
- **runtimePort**: Runtime port of PLC system, 800 to 899. TwinCAT 2 uses 800-850 and TwinCAT 3 is recommended to use 851-899. TwinCAT 2 usually has 801 as default and TwinCAT 3 uses 851
- **hostAMS**: Host AMS net ID. Usually the IP address + `.1.1`. Use `auto` to automatically derive it from the outbound TCP connection's local IP address. This is useful in Docker/Kubernetes where the container IP may not be known in advance
- **hostPort**: Host port
- **readType**: Read type for the symbols. `interval` means benthos reads all symbols at a specified interval and `notification` is a function in the PLC where benthos sends a notification request to the PLC and the PLC adds the symbol to its internal notification system and sends data whenever there is a change
- **maxDelay**: Default max delay for sending notifications in ms. Sets a maximum time for how long after the change the PLC must send the notification
- **cycleTime**: Default cycle time for notification handler in ms. Tells the notification handler how often to scan for changes. For symbols like triggers that are only true or false for 1 PLC cycle it can be necessary to use a low value
- **intervalTime**: Interval time for reading in ms. For reading batches of symbols this sets the time between readings
- **requestTimeout**: Timeout for individual ADS requests in milliseconds (default 5000). Increase this if you experience timeouts with slow PLCs or large symbol tables
- **transmissionMode**: Notification transmission mode (default `serverOnChange`). Only applies when `readType` is `notification` — ignored for `interval` mode since it uses plain ADS Read commands instead. See [Transmission Modes](#transmission-modes) below
- **upperCase**: Converts symbol names to all uppercase for older PLCs. For TwinCAT 2 this is often necessary
- **logLevel**: Log level for ADS connection. Sets the log level of the internal log function for the underlying ads library. When set to `debug` or `trace`, ADS error codes will be shown with human-readable descriptions (e.g. `0x0710: symbol not found` instead of just `1808`)
- **routeUsername**: Username for automatic UDP route registration on the PLC. If set, a route will be registered before connecting. See [Route Registration](#route-registration-docker--kubernetes)
- **routePassword**: Password for automatic UDP route registration on the PLC
- **routeHostAddress**: The IP address/hostname the PLC should use to connect back to this client. Auto-detected from the outbound connection if left empty
- **symbols**: List of symbols to read from in the format `function.variable:maxDelay:cycleTime`, e.g. `MAIN.MYTRIGGER:0:10` is a variable in the main program with 0ms max delay and 10ms cycle time, `MAIN.MYBOOL` is a variable in the main program with no extra arguments so it will use the default max delay and cycle time. `.superDuperInt` is a global variable with no extra arguments. All global variables must start with a `.` e.g. `.someStrangeVar`

#### Transmission Modes

> **Note:** `transmissionMode` only applies when `readType` is `notification`. When using `readType: interval`, the plugin sends plain ADS Read commands to the PLC at each interval — no notification mechanism is involved, and `transmissionMode` is ignored.

The `transmissionMode` field controls how the PLC's internal notification handler sends updates back to the client. The available modes are:

| Mode | Value | Description |
|------|-------|-------------|
| `serverOnChange` | 4 | **(Default)** The PLC scans for changes at the configured `cycleTime` interval and sends a notification only when the value has changed. This is the most efficient mode for most use cases. |
| `serverCycle` | 3 | The PLC sends the current value at every `cycleTime` interval, regardless of whether the value has changed. Useful when you need a constant data stream or heartbeat. |
| `serverOnChange2` | 6 | Enhanced version of `serverOnChange` available on newer TwinCAT 3 firmware. Supports more efficient internal handling on the PLC side. Falls back gracefully if not supported. |
| `serverCycle2` | 5 | Enhanced version of `serverCycle` available on newer TwinCAT 3 firmware. Same behavior as `serverCycle` but with improved internal efficiency. Falls back gracefully if not supported. |

**Choosing a mode:**
- Use `serverOnChange` (default) for event-driven data where you only care about changes
- Use `serverCycle` when you need periodic snapshots regardless of changes
- Use the `2` variants (`serverOnChange2`, `serverCycle2`) only if you are running TwinCAT 3 build 4024+ and want reduced PLC overhead. Older PLCs will return an error code `0x0713: ADS TransMode not supported` if these modes are not available

```yaml
input:
  ads:
    transmissionMode: 'serverOnChange'   # default, sends only on value change
    # transmissionMode: 'serverCycle'    # sends at every cycle regardless of change
    # transmissionMode: 'serverOnChange2' # enhanced, TwinCAT 3 only
    # transmissionMode: 'serverCycle2'    # enhanced cyclic, TwinCAT 3 only
```

#### Route Registration (Docker / Kubernetes)

When running in Docker containers or Kubernetes pods (without `host_network`), the container has its own internal IP address that the PLC doesn't know about. The ADS protocol requires a **bidirectional route** — the PLC needs to know how to send packets back to the client.

The plugin can automatically register a route on the PLC using the Beckhoff UDP route protocol (port 48899). This removes the need to manually add routes in the TwinCAT System Manager for each client.

**How it works:**
1. Before establishing the TCP connection, the plugin sends a UDP route registration packet to port 48899 on the PLC
2. The packet tells the PLC: "To reach AMS NetID X, send TCP packets to IP address Y"
3. The PLC adds this as a temporary route in its route table
4. The normal ADS TCP connection is then established over port 48898

**Understanding the Docker NAT problem:**

ADS is bidirectional — the PLC must be able to initiate TCP connections _back_ to the client. With Docker bridge networking, your container has an internal IP (e.g. `172.17.0.2`) that is **not reachable** from the PLC network. Outbound packets from the container get NAT'd to the Docker host IP, but the PLC has no way to reach the container's internal IP.

This means auto-detection of `routeHostAddress` will **not work** with default Docker bridge networking, because it discovers the container-internal IP. The plugin will log a warning if it detects this situation.

You have three options:

| Setup | How it works | Complexity |
|-------|-------------|------------|
| **Port forwarding** (recommended) | Forward port 48898 on the Docker host to the container. Set `routeHostAddress` to the Docker host IP. The PLC connects to the host, Docker NAT forwards to the container. | Low |
| **Macvlan / ipvlan network** | Give the container a real IP on the PLC network. Auto-detection works correctly. No port forwarding needed. | Medium |
| **`host_network: true`** | Container shares the host's network stack. Auto-detection works, but you lose container network isolation. Route registration is still useful to avoid manual PLC configuration. | Low |

**Configuration (port forwarding setup):**

```yaml
# docker-compose.yml
services:
  benthos:
    image: your-benthos-image
    ports:
      - "48898:48898"    # Forward ADS port so PLC can connect back
```

```yaml
# benthos.yaml
input:
  ads:
    targetIP: '192.168.1.100'
    targetAMS: '192.168.1.100.1.1'
    targetPort: 48898
    runtimePort: 851
    hostAMS: '192.168.1.50.1.1'         # Use the Docker HOST IP + .1.1
    hostPort: 10500
    routeUsername: 'Administrator'       # PLC admin username (triggers route registration)
    routePassword: '1'                  # PLC admin password
    routeHostAddress: '192.168.1.50'    # Docker HOST IP (not the container IP!)
    # ... rest of config
```

In this example, `192.168.1.50` is the Docker host's IP on the PLC network. The PLC will connect to `192.168.1.50:48898`, and Docker's port forwarding will route it to the container.

**Configuration (macvlan / host_network):**

When the container has a real IP on the PLC network (macvlan, ipvlan, or host_network), auto-detection works correctly:

```yaml
input:
  ads:
    targetIP: '192.168.1.100'
    targetAMS: '192.168.1.100.1.1'
    targetPort: 48898
    runtimePort: 851
    hostAMS: 'auto'                     # auto-derive from container's real IP
    hostPort: 10500
    routeUsername: 'Administrator'
    routePassword: '1'
    routeHostAddress: ''                # auto-detect works here
    # ... rest of config
```

**Parameters explained:**

- `routeUsername` / `routePassword`: The credentials of the PLC administrator account. These are the same credentials you would use in the TwinCAT System Manager to add a route. Setting `routeUsername` activates the automatic route registration.
- `routeHostAddress`: The IP address or hostname that the PLC should use to connect back to this client. **This must be an IP that the PLC can actually reach.** If left empty, it is auto-detected from the outbound connection's local IP — but this only works when the container has a routable IP (macvlan, host_network). With Docker bridge networking, you **must** set this to the Docker host IP and configure port forwarding.
- `hostAMS`: When set to `auto`, the AMS NetID is derived from the local IP of the outbound TCP connection (e.g. if local IP is `10.0.0.5`, the AMS NetID becomes `10.0.0.5.1.1`). When using port forwarding, set this explicitly to match the `routeHostAddress` (e.g. `192.168.1.50.1.1`).

**Network requirements:**
- UDP port 48899 must be reachable on the PLC from the client (for route registration)
- TCP port 48898 must be reachable on the PLC from the client (outbound — usually works through any NAT)
- TCP port 48898 must be reachable on the client from the PLC (inbound — requires port forwarding with Docker bridge, or a routable container IP)

**Kubernetes example:**
In Kubernetes, pods get cluster-internal IPs that are not routable from the PLC network. You need to either:
- Use a `NodePort` service exposing port 48898 and set `routeHostAddress` to the node IP
- Use `hostNetwork: true` in the pod spec
- Use a CNI plugin that assigns routable IPs to pods (e.g. Calico with BGP peering to the PLC network)

#### Output

This outputs for each address a single message with the payload being the value that was read. To distinguish messages, you can use meta("symbol_name") in a following benthos bloblang processor.

## Testing

Tested and verified:
#### cx1020, TwinCAT 2
- Read batches, Add notifications, different cycle times and max delay.
- Different datatypes, INT, INT16, UINT, DINT, BOOL, STRUCT, and more
