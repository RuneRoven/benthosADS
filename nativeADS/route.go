package ads

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

// UDP route registration constants
const (
	routePort       = 48899
	routeCookie     = 0x71146603
	routeServiceAdd = 6

	tagPassword     uint16 = 2
	tagComputerName uint16 = 5
	tagNetID        uint16 = 7
	tagRouteName    uint16 = 12
	tagUsername      uint16 = 13
	tagResponseError uint16 = 1
)

// AddRemoteRoute registers a route on the remote PLC via the Beckhoff UDP protocol (port 48899).
// This tells the PLC how to reach this client's AmsNetId.
//
// Parameters:
//   - remoteHost: IP or hostname of the PLC
//   - localNetId: the AMS NetID this client will use as source
//   - routeName: name for the route entry on the PLC
//   - computerName: the IP/hostname the PLC should use to connect back to this client
//   - username: PLC admin username (typically "Administrator")
//   - password: PLC admin password
func AddRemoteRoute(remoteHost string, localNetId [6]byte, routeName string, computerName string, username string, password string) error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", remoteHost, routePort))
	if err != nil {
		return fmt.Errorf("failed to resolve remote host: %w", err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to dial UDP: %w", err)
	}
	defer conn.Close()

	// Build the route request packet
	packet := buildRoutePacket(localNetId, routeName, computerName, username, password)

	_, err = conn.Write(packet)
	if err != nil {
		return fmt.Errorf("failed to send route request: %w", err)
	}

	// Wait for response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respBuf := make([]byte, 1024)
	n, err := conn.Read(respBuf)
	if err != nil {
		return fmt.Errorf("failed to read route response: %w", err)
	}

	return parseRouteResponse(respBuf[:n])
}

// buildRoutePacket constructs a UDP route registration packet.
func buildRoutePacket(localNetId [6]byte, routeName string, computerName string, username string, password string) []byte {
	// Build tags
	tags := [][]byte{
		buildTag(tagNetID, localNetId[:]),
		buildTag(tagPassword, appendNull([]byte(password))),
		buildTag(tagComputerName, appendNull([]byte(computerName))),
		buildTag(tagRouteName, appendNull([]byte(routeName))),
		buildTag(tagUsername, appendNull([]byte(username))),
	}

	var tagsData []byte
	for _, tag := range tags {
		tagsData = append(tagsData, tag...)
	}

	// Header: cookie(4) + invokeId(4) + serviceId(4) + tagCount(4) + AmsAddr(8)
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header[0:], routeCookie)
	binary.LittleEndian.PutUint32(header[4:], 0) // invokeId
	binary.LittleEndian.PutUint32(header[8:], routeServiceAdd)
	binary.LittleEndian.PutUint32(header[12:], uint32(len(tags)))
	// AmsAddr: NetID(6) + Port(2)
	copy(header[16:22], localNetId[:])
	binary.LittleEndian.PutUint16(header[22:], uint16(PortR0Plc))

	return append(header, tagsData...)
}

// buildTag creates a single tag: tagId(2) + length(2) + data.
func buildTag(tagId uint16, data []byte) []byte {
	tag := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint16(tag[0:], tagId)
	binary.LittleEndian.PutUint16(tag[2:], uint16(len(data)))
	copy(tag[4:], data)
	return tag
}

// appendNull appends a null terminator to a byte slice.
func appendNull(data []byte) []byte {
	return append(data, 0)
}

// parseRouteResponse validates the route registration response.
func parseRouteResponse(data []byte) error {
	if len(data) < 16 {
		return fmt.Errorf("route response too short: %d bytes", len(data))
	}

	cookie := binary.LittleEndian.Uint32(data[0:])
	if cookie != routeCookie {
		return fmt.Errorf("unexpected route response cookie: 0x%08X", cookie)
	}

	serviceId := binary.LittleEndian.Uint32(data[8:])
	if serviceId != routeServiceAdd {
		return fmt.Errorf("unexpected route response serviceId: %d", serviceId)
	}

	// Parse tags to find error code
	tagCount := binary.LittleEndian.Uint32(data[12:])
	offset := 16
	for i := uint32(0); i < tagCount && offset+4 <= len(data); i++ {
		tid := binary.LittleEndian.Uint16(data[offset:])
		tlen := binary.LittleEndian.Uint16(data[offset+2:])
		offset += 4
		if offset+int(tlen) > len(data) {
			break
		}
		if tid == tagResponseError && tlen >= 4 {
			errCode := binary.LittleEndian.Uint32(data[offset:])
			if errCode != 0 {
				return fmt.Errorf("route registration failed with error code: %d", errCode)
			}
			log.Info().Msg("route registration successful")
			return nil
		}
		offset += int(tlen)
	}

	log.Info().Msg("route registration response received (no error tag found, assuming success)")
	return nil
}
