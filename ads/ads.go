package adsPlugin

import (
	"context"
	"github.com/benthosdev/benthos/v4/public/service"

)

type adsDataItems struct {
	symbolName      string
	dataType 		converterFunc
	Item       		gos7.S7DataItem
}

// ads communication struct defines the structure for our custom Benthos input plugin.
// It holds the configuration necessary to establish a connection with a Beckhoff PLC,
// along with the read requests to fetch data from the PLC.
type adsCommInput struct {
	targetIP    string                                	// IP address of the PLC.
	targetAMS   string                                	// Target AMS net ID.
	port        int                                   	// Target port. Default 801(Twincat 2)
	hostAMS 	string                                  // The host AMS net ID, auto (default) automatically derives AMS from host IP. Enter manually if auto not working
	readType 	string									// Read type, interval or notification
	cycleTime	int										// Read interval time if read type interval, cycle time if read type notification in milliseconds
	maxDelay	int										// Max delay time after value change before PLC should send message, in milliseconds
	timeout     time.Duration                        	// Time duration before a connection attempt or read request times out.
	handler     *connection								// TCP handler to manage the connection.
	log         *service.Logger                       	// Logger for logging plugin activity.
	symbols     [][]adsDataItems 						// List of items to read from the PLC, grouped into batches with a maximum size.
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
	Field(service.NewStringListField("symbols").Description("List of symbols and data type for each to read in the format 'function.name,int32', e.g., 'MAIN.counter,int32', '.globalCounter,int64' "))


defer log.Flush()