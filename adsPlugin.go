package benthosADS

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	adsLib "github.com/RuneRoven/go-ads/v2"
	"github.com/redpanda-data/benthos/v4/public/service"
)

// benthosLogHandler bridges go-ads v2 slog-based logging into Benthos's logging infrastructure.
type benthosLogHandler struct {
	logger *service.Logger
	level  slog.Level
	attrs  []slog.Attr
}

func (h *benthosLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *benthosLogHandler) Handle(_ context.Context, r slog.Record) error {
	var kvs []any
	for _, a := range h.attrs {
		kvs = append(kvs, a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		kvs = append(kvs, a.Key, a.Value.Any())
		return true
	})
	l := h.logger
	if len(kvs) > 0 {
		l = l.With(kvs...)
	}
	switch {
	case r.Level >= slog.LevelError:
		l.Errorf("%s", r.Message)
	case r.Level >= slog.LevelWarn:
		l.Warnf("%s", r.Message)
	case r.Level >= slog.LevelInfo:
		l.Infof("%s", r.Message)
	default:
		l.Debugf("%s", r.Message)
	}
	return nil
}

func (h *benthosLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &benthosLogHandler{logger: h.logger, level: h.level, attrs: append(h.attrs, attrs...)}
}

func (h *benthosLogHandler) WithGroup(_ string) slog.Handler { return h }

// validateIP checks that s is a valid IPv4 address (4 dot-separated octets, each 0–255).
func validateIP(s string) error {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return fmt.Errorf("%q must have 4 dot-separated octets", s)
	}
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 255 {
			return fmt.Errorf("%q contains invalid octet %q (must be 0–255)", s, p)
		}
	}
	return nil
}

// validateAMSNetID checks that s is a valid AMS NetID (6 dot-separated octets, each 0–255).
func validateAMSNetID(s string) error {
	parts := strings.Split(s, ".")
	if len(parts) != 6 {
		return fmt.Errorf("%q must have 6 dot-separated octets (e.g. 192.168.1.100.1.1)", s)
	}
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 255 {
			return fmt.Errorf("%q contains invalid octet %q (must be 0–255)", s, p)
		}
	}
	return nil
}

func slogLevelFromString(level string) slog.Level {
	switch level {
	case "trace":
		return adsLib.LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.Level(100)
	}
}

type plcSymbol struct {
	name      string
	maxDelay  time.Duration
	cycleTime time.Duration
}

func sanitize(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	return re.ReplaceAllString(s, "_")
}

// isLikelyContainerIP checks if an IP address looks like a Docker/container-internal
// address that is probably not routable from an external PLC network.
func isLikelyContainerIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	p := parsed.To4()
	if p == nil {
		return false
	}
	// Docker default bridge: 172.17.0.0/16
	if p[0] == 172 && p[1] >= 17 && p[1] <= 31 {
		return true
	}
	// Common container overlay/pod networks: 10.0.0.0/8
	if p[0] == 10 {
		return true
	}
	// CGNAT range used by some Kubernetes CNIs: 100.64.0.0/10
	if p[0] == 100 && p[1] >= 64 && p[1] <= 127 {
		return true
	}
	return false
}

// createSymbolList parses symbol strings into plcSymbol structs.
// Format: "name" or "name:maxDelayMs:cycleTimeMs"
func createSymbolList(s []string, defaultCycleTime int, defaultMaxDelay int) []plcSymbol {
	var result []plcSymbol
	for _, symbol := range s {
		colons := strings.Count(symbol, ":")
		var sym plcSymbol
		if colons != 2 {
			parts := strings.Split(symbol, ":")
			if len(parts) > 0 {
				sym.name = parts[0]
			}
			sym.maxDelay = time.Duration(defaultMaxDelay) * time.Millisecond
			sym.cycleTime = time.Duration(defaultCycleTime) * time.Millisecond
		} else {
			parts := strings.Split(symbol, ":")
			sym.name = parts[0]
			maxDelay, err1 := strconv.Atoi(parts[1])
			if err1 != nil {
				maxDelay = defaultMaxDelay
			}
			cycleTime, err2 := strconv.Atoi(parts[2])
			if err2 != nil {
				cycleTime = defaultCycleTime
			}
			sym.maxDelay = time.Duration(maxDelay) * time.Millisecond
			sym.cycleTime = time.Duration(cycleTime) * time.Millisecond
		}
		result = append(result, sym)
	}
	return result
}

type adsCommInput struct {
	targetIP         string
	targetAMS        string
	targetPort       int
	runtimePort      int
	hostAMS          string
	hostPort         int
	readType         string
	cycleTime        int
	maxDelay         int
	intervalTime     time.Duration
	requestTimeout   time.Duration
	handler          *adsLib.Session
	log              *service.Logger
	symbols          []plcSymbol
	notificationChan chan *adsLib.Update
	transmissionMode adsLib.TransMode

	// Shutdown signal — closed by Close() to unblock ReadBatchNotification.
	done chan struct{}

	// Symbol metadata populated lazily after connect (from go-ads cache, no extra round-trips).
	dataTypes   map[string]string
	baseTypes   map[string]string
	dataSizes   map[string]uint32
	symbolNames map[string]string // strings.ToLower(name) → configured casing (TC2 returns uppercase)

	// Route registration settings
	routeUsername    string
	routePassword    string
	routeHostAddress string

	adsLogger *slog.Logger
}

var adsConf = service.NewConfigSpec().
	Summary("Creates an input that reads data from Beckhoff PLCs using ADS protocol. Created by Daniel H").
	Description("This input plugin enables Benthos to read data directly from Beckhoff PLCs using the ADS protocol. " +
		"Configure the plugin by specifying the PLC's IP address, runtime port, target AMS net ID, etc.").
	Field(service.NewStringField("targetIP").Description("IP address of the Beckhoff PLC.")).
	Field(service.NewStringField("targetAMS").Description("Target AMS net ID.")).
	Field(service.NewIntField("targetPort").Description("TCP port of the PLC ADS gateway.").Default(48898)).
	Field(service.NewIntField("runtimePort").Description("Target runtime port. 801 for TwinCAT 2, 851 for TwinCAT 3.").Default(801)).
	Field(service.NewStringField("hostAMS").Description("Local AMS net ID. 'auto' derives it from the outbound TCP source IP.").Default("auto")).
	Field(service.NewIntField("hostPort").Description("AMS source port used in protocol headers. Any arbitrary value works.").Default(10500)).
	Field(service.NewStringField("readType").Description("Read type, interval or notification (default).").Default("notification")).
	Field(service.NewIntField("maxDelay").Description("Max delay time after value change before PLC should send message, in milliseconds.").Default(100)).
	Field(service.NewIntField("cycleTime").Description("Requested read interval for PLC to scan for changes (notification mode), in milliseconds.").Default(1000)).
	Field(service.NewIntField("intervalTime").Description("Interval between reads in milliseconds for interval read type.").Default(1000)).
	Field(service.NewStringField("logLevel").Description("Log level for ADS connection. Default disabled.").Default("disabled")).
	Field(service.NewIntField("requestTimeout").Description("Timeout for individual ADS requests in milliseconds.").Default(5000)).
	Field(service.NewStringField("transmissionMode").Description("Notification transmission mode: serverOnChange (default), serverCycle, serverOnChange2, serverCycle2.").Default("serverOnChange")).
	Field(service.NewStringField("routeUsername").Description("Username for UDP route registration on the PLC. If set with routePassword, a route will be registered before connecting.").Default("")).
	Field(service.NewStringField("routePassword").Description("Password for UDP route registration on the PLC.").Default("")).
	Field(service.NewStringField("routeHostAddress").Description("The address the PLC should use to reach this client. Auto-detected from outbound connection if empty.").Default("")).
	Field(service.NewStringListField("symbols").Description("Symbols to read. Format: 'MAIN.var' or 'MAIN.var:maxDelayMs:cycleTimeMs'. " +
		"Examples: 'MAIN.counter', '.globalCounter', 'MAIN.var:50:100'"))

func newAdsCommInput(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchInput, error) {
	logLevel, err := conf.FieldString("logLevel")
	if err != nil {
		return nil, err
	}
	adsLogger := slog.New(&benthosLogHandler{
		logger: mgr.Logger(),
		level:  slogLevelFromString(logLevel),
	})

	targetIP, err := conf.FieldString("targetIP")
	if err != nil {
		return nil, err
	}

	targetAMS, err := conf.FieldString("targetAMS")
	if err != nil {
		return nil, err
	}

	if err = validateIP(targetIP); err != nil {
		return nil, fmt.Errorf("targetIP: %w", err)
	}
	if err = validateAMSNetID(targetAMS); err != nil {
		return nil, fmt.Errorf("targetAMS: %w", err)
	}

	targetPort, err := conf.FieldInt("targetPort")
	if err != nil {
		return nil, err
	}

	runtimePort, err := conf.FieldInt("runtimePort")
	if err != nil {
		return nil, err
	}
	if runtimePort < 0 || runtimePort > 65535 {
		return nil, fmt.Errorf("runtimePort %d out of range 0–65535", runtimePort)
	}

	hostAMS, err := conf.FieldString("hostAMS")
	if err != nil {
		return nil, err
	}
	if hostAMS != "auto" && hostAMS != "" {
		if err = validateAMSNetID(hostAMS); err != nil {
			return nil, fmt.Errorf("hostAMS: %w", err)
		}
	}

	hostPort, err := conf.FieldInt("hostPort")
	if err != nil {
		return nil, err
	}
	if hostPort < 0 || hostPort > 65535 {
		return nil, fmt.Errorf("hostPort %d out of range 0–65535", hostPort)
	}

	readType, err := conf.FieldString("readType")
	if err != nil {
		return nil, err
	}
	if readType != "notification" && readType != "interval" {
		return nil, errors.New("readType must be 'notification' or 'interval'")
	}

	maxDelay, err := conf.FieldInt("maxDelay")
	if err != nil {
		return nil, err
	}

	cycleTime, err := conf.FieldInt("cycleTime")
	if err != nil {
		return nil, err
	}

	symbols, err := conf.FieldStringList("symbols")
	if err != nil {
		return nil, err
	}

	intervalTimeInt, err := conf.FieldInt("intervalTime")
	if err != nil {
		return nil, err
	}

	requestTimeoutInt, err := conf.FieldInt("requestTimeout")
	if err != nil {
		return nil, err
	}

	transmissionModeStr, err := conf.FieldString("transmissionMode")
	if err != nil {
		return nil, err
	}
	var transmissionMode adsLib.TransMode
	switch transmissionModeStr {
	case "serverOnChange":
		transmissionMode = adsLib.TransModeServerOnChange
	case "serverCycle":
		transmissionMode = adsLib.TransModeServerCycle
	case "serverOnChange2":
		transmissionMode = adsLib.TransModeServerOnChange2
	case "serverCycle2":
		transmissionMode = adsLib.TransModeServerCycle2
	default:
		transmissionMode = adsLib.TransModeServerOnChange
	}

	routeUsername, err := conf.FieldString("routeUsername")
	if err != nil {
		return nil, err
	}

	routePassword, err := conf.FieldString("routePassword")
	if err != nil {
		return nil, err
	}

	routeHostAddress, err := conf.FieldString("routeHostAddress")
	if err != nil {
		return nil, err
	}

	// Derive hostAMS from routeHostAddress when set to "auto",
	// matching the same convenience shortcut as the integrated plugin.
	if hostAMS == "auto" && routeHostAddress != "" {
		hostAMS = routeHostAddress + ".1.1"
	}

	symbolList := createSymbolList(symbols, cycleTime, maxDelay)
	m := &adsCommInput{
		targetIP:         targetIP,
		targetAMS:        targetAMS,
		targetPort:       targetPort,
		runtimePort:      runtimePort,
		hostAMS:          hostAMS,
		hostPort:         hostPort,
		readType:         readType,
		maxDelay:         maxDelay,
		cycleTime:        cycleTime,
		symbols:          symbolList,
		log:              mgr.Logger(),
		intervalTime:     time.Duration(intervalTimeInt) * time.Millisecond,
		requestTimeout:   time.Duration(requestTimeoutInt) * time.Millisecond,
		notificationChan: make(chan *adsLib.Update, 256),
		done:             make(chan struct{}),
		transmissionMode: transmissionMode,
		routeUsername:    routeUsername,
		routePassword:    routePassword,
		routeHostAddress: routeHostAddress,
		adsLogger:        adsLogger,
	}

	return service.AutoRetryNacksBatched(m), nil
}

func init() {
	err := service.RegisterBatchInput(
		"ads", adsConf,
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchInput, error) {
			return newAdsCommInput(conf, mgr)
		})
	if err != nil {
		panic(err)
	}
}

func (g *adsCommInput) Connect(ctx context.Context) error {
	if g.handler != nil {
		return nil
	}

	if g.done == nil {
		g.done = make(chan struct{})
	}

	g.log.Infof("Creating new connection")

	var connOpts []adsLib.SessionOption
	if g.adsLogger != nil {
		connOpts = append(connOpts, adsLib.WithLogger(g.adsLogger))
		adsLib.SetDefaultLogger(g.adsLogger)
	}

	if g.routeUsername != "" && g.routePassword != "" {
		hostAddr := g.routeHostAddress
		if hostAddr == "" {
			// Use TCP connect to guarantee same source IP as the actual ADS connection.
			tcpConn, dialErr := net.DialTimeout("tcp4", net.JoinHostPort(g.targetIP, "48898"), 3*time.Second)
			if dialErr != nil {
				// PLC unreachable — fall back to UDP routing lookup (no packet sent).
				udpConn, udpErr := net.Dial("udp4", net.JoinHostPort(g.targetIP, "48899"))
				if udpErr != nil {
					g.log.Errorf("Failed to auto-detect local address: %v", dialErr)
					return dialErr
				}
				hostAddr = udpConn.LocalAddr().(*net.UDPAddr).IP.String()
				udpConn.Close()
			} else {
				hostAddr = tcpConn.LocalAddr().(*net.TCPAddr).IP.String()
				tcpConn.Close()
			}
		}
		if isLikelyContainerIP(hostAddr) {
			g.log.Warnf("Auto-detected IP %s looks like a container IP. Set routeHostAddress to the Docker host's IP for route registration to work.", hostAddr)
		}
		routeName := fmt.Sprintf("benthosADS-%s", hostAddr)
		g.log.Infof("Route will be registered on PLC %s: name=%s, clientIP=%s", g.targetIP, routeName, hostAddr)
		connOpts = append(connOpts, adsLib.WithRoute(routeName, g.routeUsername, g.routePassword))
		connOpts = append(connOpts, adsLib.WithHostIP(hostAddr))
	}

	targetAMS, err := adsLib.NewAMSAddress(g.targetAMS, uint16(g.runtimePort))
	if err != nil {
		g.log.Errorf("Invalid target AMS %q: %v", g.targetAMS, err)
		return err
	}

	if g.hostAMS != "" && g.hostAMS != "auto" {
		localAMS, lerr := adsLib.NewAMSAddress(g.hostAMS, uint16(g.hostPort))
		if lerr != nil {
			g.log.Errorf("Invalid local AMS %q: %v", g.hostAMS, lerr)
			return lerr
		}
		connOpts = append(connOpts, adsLib.WithLocalAMS(localAMS))
	}
	if g.requestTimeout > 0 {
		connOpts = append(connOpts, adsLib.WithRequestTimeout(g.requestTimeout))
	}

	// Use Background ctx for session lifetime — Benthos passes a per-call ctx to Connect
	// that would tear the session down as soon as Connect returns. Teardown is driven by Close().
	g.handler, err = adsLib.NewSession(context.Background(), adsLib.AMSEndpoint{
		IP:   g.targetIP,
		Port: g.targetPort,
		AMS:  targetAMS,
	}, connOpts...)
	if err != nil {
		g.log.Errorf("Failed to create session: %v", err)
		return err
	}

	success := false
	defer func() {
		if !success && g.handler != nil {
			_ = g.handler.Close()
			g.handler = nil
		}
	}()

	g.log.Infof("Connecting to PLC")
	if err = g.handler.Connect(ctx); err != nil {
		g.log.Errorf("Failed to connect to PLC at %s: %v", g.targetIP, err)
		return err
	}

	g.symbolNames = make(map[string]string, len(g.symbols))
	g.dataTypes = make(map[string]string, len(g.symbols))
	g.baseTypes = make(map[string]string, len(g.symbols))
	g.dataSizes = make(map[string]uint32, len(g.symbols))
	for _, sym := range g.symbols {
		g.symbolNames[strings.ToLower(sym.name)] = sym.name
	}

	if g.readType == "notification" {
		configs := make([]adsLib.NotificationConfig, len(g.symbols))
		for i, symbol := range g.symbols {
			configs[i] = adsLib.NotificationConfig{
				SymbolName:       symbol.name,
				MaxDelay:         symbol.maxDelay,
				CycleTime:        symbol.cycleTime,
				TransmissionMode: g.transmissionMode,
			}
		}

		results, err := g.handler.AddSymbolNotifications(ctx, configs, g.notificationChan)
		if err != nil {
			g.log.Errorf("Batch add notifications failed: %v", err)
			return err
		}

		registered := 0
		for i, r := range results {
			switch {
			case r.Skipped == nil && r.Error == adsLib.ReturnCodeNoErrors:
				registered++
			case r.Skipped != nil:
				g.log.Errorf("Notification symbol %q skipped (check symbol name): %v", configs[i].SymbolName, r.Skipped)
			default:
				g.log.Errorf("Notification symbol %q rejected by PLC: ADS error 0x%X", configs[i].SymbolName, uint32(r.Error))
			}
		}
		if registered == 0 && len(configs) > 0 {
			return fmt.Errorf("no symbols registered for notifications (%d symbols all failed to resolve)", len(configs))
		}
		g.log.Infof("Registered %d/%d notification symbols", registered, len(configs))

		// Wait for initial sample from each registered symbol. TwinCAT sends an
		// immediate sample on subscribe, so this completes quickly and ensures the
		// first ReadBatch returns a full batch.
		needed := make(map[string]bool, registered)
		for i, r := range results {
			if r.Skipped == nil && r.Error == adsLib.ReturnCodeNoErrors {
				needed[strings.ToLower(configs[i].SymbolName)] = true
			}
		}
		initialCtx, initialCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer initialCancel()
		for len(needed) > 0 {
			select {
			case update := <-g.notificationChan:
				if update != nil {
					delete(needed, strings.ToLower(update.Variable))
				}
			case <-initialCtx.Done():
				g.log.Warnf("Timed out waiting for initial samples; %d symbols not yet received: %v", len(needed), needed)
				goto doneWaiting
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	doneWaiting:
	}

	success = true
	return nil
}

func (g *adsCommInput) makeNotificationMessage(update *adsLib.Update) *service.Message {
	msg := service.NewMessage([]byte(update.Value))
	key := strings.ToLower(update.Variable)
	name := update.Variable
	if configured, ok := g.symbolNames[key]; ok {
		name = configured
	}
	msg.MetaSet("symbol_name", sanitize(name))
	if dt, ok := g.dataTypes[key]; ok {
		msg.MetaSet("data_type", dt)
	}
	if bt, ok := g.baseTypes[key]; ok {
		msg.MetaSet("base_type", bt)
	}
	if sz, ok := g.dataSizes[key]; ok {
		msg.MetaSet("data_size", strconv.FormatUint(uint64(sz), 10))
	}
	return msg
}

func (g *adsCommInput) ReadBatchPull(ctx context.Context) (service.MessageBatch, service.AckFunc, error) {
	g.log.Debugf("ReadBatchPull called")
	start := time.Now()
	if g.handler == nil {
		return nil, nil, service.ErrNotConnected
	}

	names := make([]string, len(g.symbols))
	for i, symbol := range g.symbols {
		names[i] = symbol.name
	}

	values, err := g.handler.ReadMultipleSymbols(ctx, names)
	if err != nil {
		g.log.Errorf("Batch read failed: %v", err)
		if g.handler.IsClosed() {
			old := g.handler
			g.handler = nil
			go func() { _ = old.Close() }()
			return nil, nil, service.ErrNotConnected
		}
		g.log.Warnf("Batch read failed (will retry): %v", err)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		return service.MessageBatch{}, func(_ context.Context, _ error) error { return nil }, nil
	}

	// Lazily populate type metadata from go-ads cache (no extra round-trips).
	for _, sym := range g.symbols {
		key := strings.ToLower(sym.name)
		if _, ok := g.dataTypes[key]; !ok {
			if view, viewErr := g.handler.GetSymbol(ctx, sym.name); viewErr == nil {
				g.dataTypes[key] = view.DataType
				g.dataSizes[key] = view.Length
				if bt := view.BaseTypeName(); bt != "" {
					g.baseTypes[key] = bt
				}
			}
		}
	}

	msgs := service.MessageBatch{}
	for _, symbol := range g.symbols {
		val, ok := values[symbol.name]
		if !ok {
			continue
		}
		key := strings.ToLower(symbol.name)
		valueMsg := service.NewMessage([]byte(val))
		valueMsg.MetaSet("symbol_name", sanitize(symbol.name))
		if dt, ok := g.dataTypes[key]; ok {
			valueMsg.MetaSet("data_type", dt)
		}
		if bt, ok := g.baseTypes[key]; ok {
			valueMsg.MetaSet("base_type", bt)
		}
		if sz, ok := g.dataSizes[key]; ok {
			valueMsg.MetaSet("data_size", strconv.FormatUint(uint64(sz), 10))
		}
		msgs = append(msgs, valueMsg)
	}

	// Some PLCs don't support ADS sum read — fall back to individual reads.
	if len(msgs) == 0 && len(g.symbols) > 0 {
		g.log.Warnf("Batch read returned no results for %d symbols, falling back to individual reads", len(g.symbols))
		for _, symbol := range g.symbols {
			val, readErr := g.handler.ReadFromSymbol(ctx, symbol.name)
			if readErr != nil {
				g.log.Errorf("Individual read failed for %s: %v", symbol.name, readErr)
				continue
			}
			key := strings.ToLower(symbol.name)
			valueMsg := service.NewMessage([]byte(val))
			valueMsg.MetaSet("symbol_name", sanitize(symbol.name))
			if dt, ok := g.dataTypes[key]; ok {
				valueMsg.MetaSet("data_type", dt)
			}
			if bt, ok := g.baseTypes[key]; ok {
				valueMsg.MetaSet("base_type", bt)
			}
			if sz, ok := g.dataSizes[key]; ok {
				valueMsg.MetaSet("data_size", strconv.FormatUint(uint64(sz), 10))
			}
			msgs = append(msgs, valueMsg)
		}
	}

	if remaining := g.intervalTime - time.Since(start); remaining > 0 {
		select {
		case <-time.After(remaining):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return msgs, func(_ context.Context, _ error) error { return nil }, nil
}

func (g *adsCommInput) ReadBatchNotification(ctx context.Context) (service.MessageBatch, service.AckFunc, error) {
	g.log.Debugf("ReadBatchNotification called")

	// Short-lived context so ReadBatch returns periodically even with slow-changing symbols.
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var first *adsLib.Update
	select {
	case first = <-g.notificationChan:
		if first == nil {
			g.log.Warnf("Received nil update from ADS library, skipping")
			return nil, func(_ context.Context, _ error) error { return nil }, nil
		}
	case <-g.done:
		return nil, nil, service.ErrEndOfInput
	case <-waitCtx.Done():
		if g.handler != nil && g.handler.IsClosed() {
			_ = g.handler.Close()
			g.handler = nil
			return nil, nil, service.ErrNotConnected
		}
		return nil, func(_ context.Context, _ error) error { return nil }, nil
	}

	msgs := service.MessageBatch{g.makeNotificationMessage(first)}

	// Drain all pending notifications without blocking to keep the channel buffer available.
	for {
		select {
		case update := <-g.notificationChan:
			if update != nil {
				msgs = append(msgs, g.makeNotificationMessage(update))
			}
		default:
			return msgs, func(_ context.Context, _ error) error { return nil }, nil
		}
	}
}

func (g *adsCommInput) ReadBatch(ctx context.Context) (service.MessageBatch, service.AckFunc, error) {
	g.log.Infof("ReadBatch called")
	if g.readType == "notification" {
		return g.ReadBatchNotification(ctx)
	}
	return g.ReadBatchPull(ctx)
}

// Close shuts down the ADS connection.
//
//nolint:revive
func (g *adsCommInput) Close(ctx context.Context) error {
	g.log.Infof("Close called")
	if g.done != nil {
		close(g.done)
		g.done = nil
	}
	if g.handler != nil {
		g.log.Infof("Closing down, cleaning up PLC handles")
		if cerr := g.handler.Close(); cerr != nil {
			g.log.Warnf("Handler close error: %v", cerr)
		}
		g.handler = nil
	}
	return nil
}
