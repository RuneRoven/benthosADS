package ads

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// SumNotificationRequest represents a single notification add request within a batch.
type SumNotificationRequest struct {
	Group            uint32
	Offset           uint32
	Length           uint32
	TransmissionMode TransMode
	MaxDelay         time.Duration
	CycleTime        time.Duration
}

// SumAddDeviceNotification adds multiple device notifications in a single ADS round-trip
// using GroupSumupAddDeviceNotification (0xF085).
func (conn *Connection) SumAddDeviceNotification(requests []SumNotificationRequest) (handles []uint32, errors []ReturnCode, err error) {
	if len(requests) == 0 {
		return nil, nil, nil
	}

	n := len(requests)

	// Each request: Group(4) + Offset(4) + Length(4) + TransMode(4) + MaxDelay(4) + CycleTime(4) + Reserved(16) = 40 bytes
	writeData := make([]byte, n*40)
	for i, req := range requests {
		off := i * 40
		binary.LittleEndian.PutUint32(writeData[off:], req.Group)
		binary.LittleEndian.PutUint32(writeData[off+4:], req.Offset)
		binary.LittleEndian.PutUint32(writeData[off+8:], req.Length)
		binary.LittleEndian.PutUint32(writeData[off+12:], uint32(req.TransmissionMode))
		binary.LittleEndian.PutUint32(writeData[off+16:], uint32(req.MaxDelay.Nanoseconds()/100))
		binary.LittleEndian.PutUint32(writeData[off+20:], uint32(req.CycleTime.Nanoseconds()/100))
		// bytes 24-39 are reserved (zero)
	}

	// Response: N × (error(4) + handle(4)) = N × 8 bytes
	readLen := uint32(n * 8)

	resp, err := conn.WriteRead(uint32(GroupSumupAddDeviceNotification), uint32(n), readLen, writeData)
	if err != nil {
		return nil, nil, fmt.Errorf("SumAddDeviceNotification failed: %w", err)
	}

	if len(resp) < n*8 {
		return nil, nil, fmt.Errorf("SumAddDeviceNotification response too short: got %d bytes, expected %d", len(resp), n*8)
	}

	handles = make([]uint32, n)
	errors = make([]ReturnCode, n)
	for i := 0; i < n; i++ {
		errors[i] = ReturnCode(binary.LittleEndian.Uint32(resp[i*8:]))
		handles[i] = binary.LittleEndian.Uint32(resp[i*8+4:])
	}

	return handles, errors, nil
}

// SumDeleteDeviceNotification deletes multiple device notifications in a single ADS round-trip
// using GroupSumupDeleteDeviceNotification (0xF086).
func (conn *Connection) SumDeleteDeviceNotification(handles []uint32) ([]ReturnCode, error) {
	if len(handles) == 0 {
		return nil, nil
	}

	n := len(handles)

	// Write data: N × handle(4)
	writeData := make([]byte, n*4)
	for i, h := range handles {
		binary.LittleEndian.PutUint32(writeData[i*4:], h)
	}

	// Response: N × error(4)
	readLen := uint32(n * 4)

	resp, err := conn.WriteRead(uint32(GroupSumupDeleteDeviceNotification), uint32(n), readLen, writeData)
	if err != nil {
		return nil, fmt.Errorf("SumDeleteDeviceNotification failed: %w", err)
	}

	if len(resp) < n*4 {
		return nil, fmt.Errorf("SumDeleteDeviceNotification response too short: got %d bytes, expected %d", len(resp), n*4)
	}

	errors := make([]ReturnCode, n)
	for i := 0; i < n; i++ {
		errors[i] = ReturnCode(binary.LittleEndian.Uint32(resp[i*4:]))
	}

	// Clean up internal notification tracking
	conn.symbolLock.Lock()
	for i, h := range handles {
		if errors[i] == ReturnCodeNoErrors {
			delete(conn.activeNotifications, h)
			log.Info().Uint32("handle", h).Msg("batch deleted notification handle")
		}
	}
	conn.symbolLock.Unlock()

	return errors, nil
}
