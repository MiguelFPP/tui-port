package procfs

import (
	"bytes"
	"encoding/hex"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

// netLineFields is the minimum number of columns a row of a /proc/net table must have.
const netLineFields = 10

// udpConnected and udpBound are the two UDP states the kernel documents; a bound datagram socket
// is written as 07, which for TCP means "close".
const (
	udpConnected = 0x01
	udpBound     = 0x07
)

// parseNetFile decodes one /proc/net table, skipping the header line and every malformed row.
//
// It keeps at most limit sockets and reports whether rows were dropped because of that limit.
func parseNetFile(data []byte, protocol domain.Protocol, limit int) ([]domain.Socket, bool) {
	sockets := make([]domain.Socket, 0, 64)

	for index, raw := range bytes.Split(data, []byte{'\n'}) {
		if index == 0 {
			continue // the kernel always writes a header first
		}

		socket, ok := parseNetLine(strings.Fields(string(raw)), protocol)
		if !ok {
			continue
		}
		if len(sockets) >= limit {
			return sockets, true
		}

		sockets = append(sockets, socket)
	}

	return sockets, false
}

// parseNetLine turns one row of a table into a socket; ok is false for the header and for a row
// the adapter cannot decode.
func parseNetLine(fields []string, protocol domain.Protocol) (domain.Socket, bool) {
	if len(fields) < netLineFields {
		return domain.Socket{}, false
	}

	local, ok := decodeEndpoint(fields[1], protocol)
	if !ok {
		return domain.Socket{}, false
	}

	remote, ok := decodeEndpoint(fields[2], protocol)
	if !ok {
		return domain.Socket{}, false
	}

	code, err := strconv.ParseUint(fields[3], 16, 8)
	if err != nil {
		return domain.Socket{}, false
	}

	uid, err := strconv.ParseUint(fields[7], 10, 32)
	if err != nil {
		return domain.Socket{}, false
	}

	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return domain.Socket{}, false
	}

	return domain.Socket{
		Protocol: protocol,
		Local:    local,
		Remote:   remote,
		State:    stateOf(protocol, uint8(code)),
		UID:      uint32(uid),
		Inode:    inode,
	}, true
}

// decodeEndpoint decodes "HHHHHHHH:PPPP", the kernel hex spelling of an address and a port.
func decodeEndpoint(raw string, protocol domain.Protocol) (domain.Endpoint, bool) {
	host, portText, ok := strings.Cut(raw, ":")
	if !ok {
		return domain.Endpoint{}, false
	}

	port, err := strconv.ParseUint(portText, 16, 16)
	if err != nil {
		return domain.Endpoint{}, false
	}

	address, ok := decodeAddress(host, protocol)
	if !ok {
		return domain.Endpoint{}, false
	}

	return domain.Endpoint{Addr: net.IP(address).String(), Port: uint16(port)}, true
}

// decodeAddress undoes the kernel byte order: the four bytes of an IPv4 address and each of the
// four 32-bit words of an IPv6 address are written little-endian.
func decodeAddress(host string, protocol domain.Protocol) ([]byte, bool) {
	address, err := hex.DecodeString(host)
	if err != nil {
		return nil, false
	}

	switch {
	case protocol == domain.ProtocolTCP6 || protocol == domain.ProtocolUDP6:
		if len(address) != net.IPv6len {
			return nil, false
		}

		for word := 0; word < net.IPv6len; word += 4 {
			slices.Reverse(address[word : word+4])
		}
	default:
		if len(address) != net.IPv4len {
			return nil, false
		}

		slices.Reverse(address)
	}

	return address, true
}

// stateOf maps the state code of one row onto the domain state, with UDP reusing two codes.
func stateOf(protocol domain.Protocol, code uint8) domain.SocketState {
	if protocol.IsUDP() {
		switch code {
		case udpBound:
			return domain.StateListen
		case udpConnected:
			return domain.StateEstablished
		}
	}

	switch code {
	case 0x01:
		return domain.StateEstablished
	case 0x02:
		return domain.StateSynSent
	case 0x03:
		return domain.StateSynRecv
	case 0x04:
		return domain.StateFinWait1
	case 0x05:
		return domain.StateFinWait2
	case 0x06:
		return domain.StateTimeWait
	case 0x07:
		return domain.StateClose
	case 0x08:
		return domain.StateCloseWait
	case 0x09:
		return domain.StateLastAck
	case 0x0A:
		return domain.StateListen
	case 0x0B:
		return domain.StateClosing
	case 0x0C:
		return domain.StateNewSynRecv
	default:
		return domain.StateUnknown
	}
}
