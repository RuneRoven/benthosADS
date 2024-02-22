package adsPlugin

import (
	"context"
	"fmt"
	"github.com/benthosdev/benthos/v4/public/service"
	ads "gitlab.com/xilix-systems-llc/go-native-ads/v4" 
)


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
	handler     	*goADS.connection						// TCP handler to manage the connection.
	log         	*service.Logger                       	// Logger for logging plugin activity.
	symbols     	[]string 								// List of items to read from the PLC, grouped into batches with a maximum size.
	deviceInfo 		DeviceInfo								// PLC device info 
	deviceSymbols	ADSSymbol								// Received symbols from PLC
	notificationChan chan *ads.Update
}


var adsConf = service.NewConfigSpec().
	Summary("Creates an input that reads data from Beckhoff PLCs using ADS. Created by Daniel H & maintained by the United Manufacturing Hub. About us: www.umh.app").
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
	
		return service.AutoRetryNacks(&m), nil
	}

	func init() {
		err := service.RegisterBatchInput(
			"adsComm", adsConf,
			func(conf *service.ParsedConfig, mgr *service.Resources) (service.AckFunc, error) {
				return newAdsCommInput(conf, mgr)
			})
		if err != nil {
			panic(err)
		}
	}

	func (g *adsCommInput) Connect(ctx context.Context) error { 
		ctx, cancel := context.WithCancel(context.Background())
		g.handler, e := goADS.NewConnection(ctx, g.targetIP, 48898, g.targetAMS, g.port, g.hostAMS, 10500)
		//g.handler.Timeout = g.timeout
		//g.handler.IdleTimeout = g.timeout
	
		err := g.handler.Connect(false)
		if err != nil {
			g.log.Errorf("Failed to connect to  PLC at %s: %v", g.targetIP, err)
			return err
		}
		defer g.handler.Close() // Close the connection when we are done

		g.DeviceInfo, e := g.handler.ReadDeviceInfo()
		if e != nil {
			log.Critical(e)
			os.Exit(1)
		}
		log.Infof("Successfully conncected to \"%s\" version %d.%d (build %d)", g.DeviceInfo.DeviceName, g.DeviceInfo.MajorVersion, g.DeviceInfo.MinorVersion, g.DeviceInfo.BuildVersion) /*}}}*/

		//g.deviceSymbols, _ := g.handler.GetSymbol()
		//g.handler.AddSymbolNotification("MAIN.I", update)
		for _, symbolName := range g.symbols {
			g.handler.AddSymbolNotification(symbolName, update)
		}
		/*
		for _, symbolName := range g.symbols {
			currentSymbol, ok := g.deviceSymbols[symbolName]
			if !ok {
				// Print an error if the symbol is missing
				fmt.Printf("Error: Symbol %s not found\n", symbolName)
				continue
			}
	
			// Add device notification and set value based on your logic
			currentSymbol.AddDeviceNotification(func(currentSymbol *goADS.ADSSymbol) {
				val := g.handler.Value(symbolName)
				if val == "True" {
					connection.Set(symbolName, "1")
				} else {
					connection.Set(symbolName, "0")
				}
				currentSymbol.Walk()
			})
		}
		*/
		g.notificationChan = make(chan *ads.Update)
		

		return nil
	}
	func (g *adsCommInput) adsResponse(ctx context.Context) (service.Message, service.AckFunc, error) {
		var res *adsCommInput.notificationChan	
		msgs := service.Message{}
		msgs = append(msgs, message)
		return msgs, func(ctx context.Context, err error) error {
		// Nacks are retried automatically when we use service.AutoRetryNacks
			return nil
		}, nil
	}

	func (g *adsCommInput) Close(ctx context.Context) error {
		if g.handler != nil {
			g.handler.Close()
			g.handler = nil
			
		}
	
		return nil
	}