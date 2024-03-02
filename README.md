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
When conneting to a ADS device you connect to a router which then routes the traffic to the correct device using the AMS net ID.
There are basically 2 ways for setting up the connection. One is to use the Twincat connection manager locally on the host, scan for the device and add a connection using the correct credentials for the PLC. The other way is to logon to the PLC using the Twincat system manager and add a static route from the PLC to the client. This is the preferred way when using benthos on a Kubernetes cluster since you have no good way of installing the connection manager.

  #### Configuration Parameters
- **targetIP**: IP address of the PLC
- **targetAMS**: AMS net ID of the target
- **targetPort**: Port of the target internal gateway
- **runtimePort**: Runtime port of PLC system,  800 to 899. Twincat 2 uses 800 - 850 and Twincat 3 is recommended to use 851-899. Twincat 2 usually have 801 as default and Twincat 3 uses 851
- **hostAMS**: Host AMS net ID. Usually the IP address + .1.1
- **hostPort**: Host port
- **readType**: Read type for the symbols. Interval means benthos reads all symbols at a specified interval and notification is a function in the PLC where benthos sends a notification request to the PLC and the PLC adds the symbol to its internal notification system and sends data whenever there is a change.
- **maxDelay**: Default ax delay for sending notifications in ms. Sets a maximum time for how long after the change the PLC must send the notification
- **cycleTime**: Default cycle time for notification handler in ms. Tells the notification handler how often to scan for changes. For symbols like triggers that is only true or false for 1 PLC cycle it can be necessary to use a low value.
- **intervalTime**: Interval time for reading in ms. For reading batches of symbols this sets the time between readings
- **upperCase**: Converts symbol names to all uppercase for older PLCs. For Twincat 2 this is often necessary.
- **logLevel**: Log level for ADS connection sets the log level of the internal log function for the underlying ads library
- **symbols**: List of symbols to read from in the format <function.variable:maxDelay:cycleTime>, eg. "MAIN.MYTRIGGER:0:10" is a variable in the main program with 0ms max delay and 10ms cycle time,  "MAIN.MYBOOL" is a variable in the main program with no extra arguments so it will use the default max delay and cycle time. ".superDuperInt" is a global variable variable with no extra arguments. All global variables must start with a <.> eg. ".someStrangeVar"

#### Output

Similar to the OPC UA input, this outputs for each address a single message with the payload being the value that was read. To distinguish messages, you can use meta("symbol_name") in a following benthos bloblang processor.

## Testing

We execute automated tests and verify that benthos-ads works:
#### cx1020, Twincat 2 
- Read batches, Add notifications, different cycle times and max delay.
- Different datatypes, INT, INT16, UINT, DINT, BOOL, STRUCT, and more
