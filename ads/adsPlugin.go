package adsPlugin

import (
	"context"
	"time"
	"sync"
	"fmt"
	"github.com/benthosdev/benthos/v4/public/service"
	adsLib "gitlab.com/xilix-systems-llc/go-native-ads/v4" 
)
var waitGroup sync.WaitGroup

// ads communication struct defines the structure for our custom Benthos input plugin.
// It holds the configuration necessary to establish a connection with a Beckhoff PLC,
// along with the read requests to fetch data from the PLC.
type adsCommInput struct {
	targetIP    	string                                	// IP address of the PLC.
	targetAMS   	string                                	// Target AMS net ID.
	port        	int                                   	// Target port. Default 801(Twincat 2)
	hostAMS 		string                                  // The host AMS net ID, auto (default) automatically derives AMS from host IP. Enter manually if auto not working
	readType 		string									// Read type, interval or notification
	cycleTime		int										// Read interval time if read type interval, cycle time if read type notification in milliseconds
	maxDelay		int										// Max delay time after value change before PLC should send message, in milliseconds
	timeout     	time.Duration                        	// Time duration before a connection attempt or read request times out.
	handler     	*adsLib.Connection							// TCP handler to manage the connection.
	log         	*service.Logger                       	// Logger for logging plugin activity.
	symbols     	[]string 								// List of items to read from the PLC, grouped into batches with a maximum size.
	deviceInfo 		adsLib.DeviceInfo								// PLC device info 
	deviceSymbols	adsLib.Symbol									// Received symbols from PLC
	notificationChan chan *adsLib.Update
}


var adsConf = service.NewConfigSpec().
	Summary("Creates an input that reads data from Beckhoff PLCs using adsLib. Created by Daniel H & maintained by the United Manufacturing Hub. About us: www.umh.app").
	Description("This input plugin enables Benthos to read data directly from Beckhoff PLCs using the ADS protocol. " +
		"Configure the plugin by specifying the PLC's IP address, runtime port, target AMS net ID, etc. etc, add more here.").
	Field(service.NewStringField("targetIP").Description("IP address of the Beckhoff PLC.")).
	Field(service.NewStringField("targetAMS").Description("Target AMS net ID.")).
	Field(service.NewIntField("port").Description("Target port. Default 801(Twincat 2)").Default(801)).
	Field(service.NewStringField("hostAMS").Description("The host AMS net ID, use auto (default) to automatically derive AMS from host IP. Enter manually if auto not working").Default("auto")).
	Field(service.NewStringField("readType").Description("Read type, interval or notification (default)").Default("notification")).
	Field(service.NewIntField("cycleTime").Description("Read interval time if read type interval, cycle time if read type notification in milliseconds.").Default(1000)).
	Field(service.NewIntField("maxDelay").Description("Max delay time after value change before PLC should send message, in milliseconds. Default 100").Default(100)).
	Field(service.NewIntField("timeout").Description("The timeout duration in seconds for connection attempts and read requests.").Default(10)).
	Field(service.NewStringListField("symbols").Description("List of symbols to read in the format 'function.name', e.g., 'MAIN.counter', '.globalCounter' "))
	//Field(service.NewStringListField("symbols").Description("List of symbols and data type for each to read in the format 'function.name,int32', e.g., 'MAIN.counter,int32', '.globalCounter,int64' "))


func newAdsCommInput(conf *service.ParsedConfig, mgr *service.Resources) (service.Input, error) {
	targetIP, err := conf.FieldString("targetIP")
	if err != nil {
		return nil, err
	}

	targetAMS, err := conf.FieldString("targetAMS")
	if err != nil {
		return nil, err
	}

	port, err := conf.FieldInt("port")
	if err != nil {
		return nil, err
	}

	hostAMS, err := conf.FieldString("hostAMS")
	if err != nil {
		return nil, err
	}

	readType, err := conf.FieldString("readType")
	if err != nil {
		return nil, err
	}

	cycleTime, err := conf.FieldInt("cycleTime")
	if err != nil {
		return nil, err
	}
	maxDelay, err := conf.FieldInt("maxDelay")
	if err != nil {
		return nil, err
	}
	
	symbols, err := conf.FieldStringList("symbols")
	if err != nil {
		return nil, err
	}

	timeoutInt, err := conf.FieldInt("timeout")
	if err != nil {
		return nil, err
	}

	//for _, value := range slice {
	//	fmt.Printf("Value: %s\n", value)
	//}

	m := &adsCommInput{
		targetIP:   	targetIP,
		targetAMS:  	targetAMS,
		port:       	port,
		hostAMS:		hostAMS,
		readType: 		readType,
		cycleTime:		cycleTime,
		maxDelay:		maxDelay,
		symbols:		symbols,
		log:          	mgr.Logger(),
		timeout:      	time.Duration(timeoutInt) * time.Second,
	}

	return service.AutoRetryNacks(m), nil
}

func init() {
	err := service.RegisterInput(
		"adsinput", adsConf,
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.Input, error) {
			return newAdsCommInput(conf, mgr)
		})
	if err != nil {
		panic(err)
	}
}

func (g *adsCommInput) Connect(ctx context.Context) error {
    // Create a new connection
    var err error
    g.handler, err = adsLib.NewConnection(ctx, g.targetIP, 48898, g.targetAMS, g.port, g.hostAMS, 10500)
    if err != nil {
        g.log.Errorf("Failed to create connection: %v", err)
        return err
    }

    // Use a WaitGroup to wait for the goroutine to complete
    var wg sync.WaitGroup
    wg.Add(1)

    // Use a mutex to synchronize access to shared variables
    var mu sync.Mutex

    // Connect to the PLC in a goroutine
    go func() {
        defer wg.Done()

        // Wait for the context to be canceled before connecting
        if ctx.Err() != nil {
            g.log.Errorf("Connection operation canceled due to context deadline exceeded")
            return
        }

        // Connect to the PLC
        err := g.handler.Connect(false)
        if err != nil {
            g.log.Errorf("Failed to connect to PLC at %s: %v", g.targetIP, err)
            return
        }
		
        // Read device info
        g.log.Infof("Read device info")
        g.deviceInfo, err = g.handler.ReadDeviceInfo()
        if err != nil {
            g.log.Errorf("Failed to read device info: %v", err)
            return
        }
        g.log.Infof("Successfully connected to \"%s\" version %d.%d (build %d)", g.deviceInfo.DeviceName, g.deviceInfo.Major, g.deviceInfo.Minor, g.deviceInfo.Version)

        // Ensure the notificationChan is initialized
        mu.Lock()
        defer mu.Unlock()
        if g.notificationChan == nil {
            g.notificationChan = make(chan *adsLib.Update)
        }

        // Add symbol notifications
        for _, symbolName := range g.symbols {
            g.log.Infof("Adding symbol notification for %s", symbolName)
            g.handler.AddSymbolNotification(symbolName, g.notificationChan)
        }
		
    }()

    // Wait for the goroutine to complete and check for errors
    wg.Wait()
    if ctx.Err() != nil {
        g.log.Errorf("Connection operation canceled due to context deadline exceeded")
        return ctx.Err()
    }

    return nil
}


func (g *adsCommInput) Read(ctx context.Context) (*service.Message, service.AckFunc, error) {
	//var res *adsCommInput.notificationChan	
	if g.handler == nil {
		// Handle the case where g.handler is nil, e.g., return an error
		return nil, nil, fmt.Errorf("handler is nil")
	}

	res := <-g.notificationChan
	//msgs := service.Message{}
	message := service.NewMessage([]byte(res.Value))
	//msgs = append(msgs, message)
	return message, func(ctx context.Context, err error) error {
	// Nacks are retried automatically when we use service.AutoRetryNacks
		return nil
	}, nil
}




func (g *adsCommInput) Close(ctx context.Context) error {
	if g.handler != nil {
		close(g.notificationChan)
        g.handler.Close()
        g.handler = nil
	}

	return nil
}