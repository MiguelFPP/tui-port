package procfs

import (
	"strconv"
	"strings"
	"testing"

	"github.com/MiguelFPP/tui-port/internal/domain"
)

// netHeader is the header the kernel writes first in every /proc/net table.
const netHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// netRow renders one row of a /proc/net table with the columns the adapter reads.
func netRow(local, remote, state string, uid uint32, inode uint64) string {
	return "   0: " + local + " " + remote + " " + state +
		" 00000000:00000000 02:00000E20 00000000 " +
		strconv.FormatUint(uint64(uid), 10) + "        0 " +
		strconv.FormatUint(inode, 10) + " 2 0000000000000000 20 4 30 10 -1\n"
}

func TestDecodeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		protocol domain.Protocol
		want     domain.Endpoint
	}{
		{
			name:     "ipv4 wildcard",
			raw:      "00000000:0050",
			protocol: domain.ProtocolTCP,
			want:     domain.Endpoint{Addr: "0.0.0.0", Port: 80},
		},
		{
			name:     "ipv4 loopback",
			raw:      "0100007F:0016",
			protocol: domain.ProtocolTCP,
			want:     domain.Endpoint{Addr: "127.0.0.1", Port: 22},
		},
		{
			name:     "ipv4 udp",
			raw:      "0100007F:1F90",
			protocol: domain.ProtocolUDP,
			want:     domain.Endpoint{Addr: "127.0.0.1", Port: 8080},
		},
		{
			name:     "ipv6 wildcard",
			raw:      "00000000000000000000000000000000:0050",
			protocol: domain.ProtocolTCP6,
			want:     domain.Endpoint{Addr: "::", Port: 80},
		},
		{
			name:     "ipv6 loopback",
			raw:      "00000000000000000000000001000000:0016",
			protocol: domain.ProtocolTCP6,
			want:     domain.Endpoint{Addr: "::1", Port: 22},
		},
		{
			name:     "ipv6 global",
			raw:      "B80D0120000000000000000001000000:14E9",
			protocol: domain.ProtocolUDP6,
			want:     domain.Endpoint{Addr: "2001:db8::1", Port: 5353},
		},
		{
			name:     "ipv6 mapped renders as ipv4",
			raw:      "0000000000000000FFFF000004030201:0050",
			protocol: domain.ProtocolTCP6,
			want:     domain.Endpoint{Addr: "1.2.3.4", Port: 80},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := decodeEndpoint(test.raw, test.protocol)
			if !ok {
				t.Fatalf("decodeEndpoint(%q) ok = false, want true", test.raw)
			}
			if got != test.want {
				t.Errorf("decodeEndpoint(%q) = %+v, want %+v", test.raw, got, test.want)
			}
		})
	}
}

func TestDecodeEndpointRejectsMalformed(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		protocol domain.Protocol
	}{
		{name: "empty", raw: "", protocol: domain.ProtocolTCP},
		{name: "no port", raw: "0100007F", protocol: domain.ProtocolTCP},
		{name: "empty port", raw: "0100007F:", protocol: domain.ProtocolTCP},
		{name: "port not hex", raw: "0100007F:XYZW", protocol: domain.ProtocolTCP},
		{name: "port overflow", raw: "0100007F:10000", protocol: domain.ProtocolTCP},
		{name: "address not hex", raw: "ZZZZZZZZ:0016", protocol: domain.ProtocolTCP},
		{name: "address too short", raw: "01007F:0016", protocol: domain.ProtocolTCP},
		{name: "ipv6 address in a v4 table", raw: "00000000000000000000000000000000:0050", protocol: domain.ProtocolTCP},
		{name: "ipv4 address in a v6 table", raw: "0100007F:0016", protocol: domain.ProtocolTCP6},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := decodeEndpoint(test.raw, test.protocol); ok {
				t.Errorf("decodeEndpoint(%q) = %+v, want el valor cero", test.raw, got)
			}
		})
	}
}

func TestStateOf(t *testing.T) {
	tests := []struct {
		code     uint8
		protocol domain.Protocol
		want     domain.SocketState
	}{
		{code: 0x01, protocol: domain.ProtocolTCP, want: domain.StateEstablished},
		{code: 0x02, protocol: domain.ProtocolTCP, want: domain.StateSynSent},
		{code: 0x03, protocol: domain.ProtocolTCP, want: domain.StateSynRecv},
		{code: 0x04, protocol: domain.ProtocolTCP, want: domain.StateFinWait1},
		{code: 0x05, protocol: domain.ProtocolTCP, want: domain.StateFinWait2},
		{code: 0x06, protocol: domain.ProtocolTCP, want: domain.StateTimeWait},
		{code: 0x07, protocol: domain.ProtocolTCP, want: domain.StateClose},
		{code: 0x08, protocol: domain.ProtocolTCP, want: domain.StateCloseWait},
		{code: 0x09, protocol: domain.ProtocolTCP, want: domain.StateLastAck},
		{code: 0x0A, protocol: domain.ProtocolTCP6, want: domain.StateListen},
		{code: 0x0B, protocol: domain.ProtocolTCP, want: domain.StateClosing},
		{code: 0x0C, protocol: domain.ProtocolTCP, want: domain.StateNewSynRecv},
		{code: 0x00, protocol: domain.ProtocolTCP, want: domain.StateUnknown},
		{code: 0x0D, protocol: domain.ProtocolTCP, want: domain.StateUnknown},
		{code: 0xFF, protocol: domain.ProtocolTCP, want: domain.StateUnknown},
		{code: 0x07, protocol: domain.ProtocolUDP, want: domain.StateListen},
		{code: 0x07, protocol: domain.ProtocolUDP6, want: domain.StateListen},
		{code: 0x01, protocol: domain.ProtocolUDP, want: domain.StateEstablished},
		{code: 0x06, protocol: domain.ProtocolUDP, want: domain.StateTimeWait},
	}

	for _, test := range tests {
		if got := stateOf(test.protocol, test.code); got != test.want {
			t.Errorf("stateOf(%s, %#02x) = %v, want %v", test.protocol, test.code, got, test.want)
		}
	}
}

func TestParseNetLine(t *testing.T) {
	const line = "  17: 0100007F:1F90 0100007F:C350 01 00000000:00000000 02:00000E20 00000000" +
		"  1000        0 1234567 2 0000000000000000 20 4 30 10 -1"

	got, ok := parseNetLine(strings.Fields(line), domain.ProtocolTCP)
	if !ok {
		t.Fatalf("parseNetLine() ok = false, want true")
	}

	want := domain.Socket{
		Protocol: domain.ProtocolTCP,
		Local:    domain.Endpoint{Addr: "127.0.0.1", Port: 8080},
		Remote:   domain.Endpoint{Addr: "127.0.0.1", Port: 50000},
		State:    domain.StateEstablished,
		UID:      1000,
		Inode:    1234567,
	}
	if got != want {
		t.Errorf("parseNetLine() = %+v, want %+v", got, want)
	}
	if got.Attributed() {
		t.Error("parseNetLine() atribuyó el socket a un pid, want 0")
	}
}

func TestParseNetLineRejectsShortRows(t *testing.T) {
	rows := []string{
		strings.TrimSpace(netHeader),
		"",
		"   0: 00000000:0016 00000000:0000 0A 00000000:00000000",
		"   0: not-an-endpoint 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 1 2",
		"   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 not-an-inode",
	}

	for _, row := range rows {
		if got, ok := parseNetLine(strings.Fields(row), domain.ProtocolTCP); ok {
			t.Errorf("parseNetLine(%q) = %+v, want ok = false", row, got)
		}
	}
}

func TestParseNetFile(t *testing.T) {
	data := netHeader +
		netRow("00000000:1F90", "00000000:0000", "0A", 0, 300001) +
		"   this row is malformed\n" +
		netRow("0100007F:C350", "0100007F:1F91", "01", 1000, 300002)

	sockets, dropped := parseNetFile([]byte(data), domain.ProtocolTCP, domain.MaxSockets)
	if dropped {
		t.Error("parseNetFile() dropped = true, want false")
	}
	if len(sockets) != 2 {
		t.Fatalf("parseNetFile() = %d sockets, want 2", len(sockets))
	}
	if sockets[0].Inode != 300001 || sockets[1].Inode != 300002 {
		t.Errorf("inodos = %d, %d, want 300001, 300002", sockets[0].Inode, sockets[1].Inode)
	}

	sockets, dropped = parseNetFile([]byte(data), domain.ProtocolTCP, 1)
	if !dropped {
		t.Error("parseNetFile(limit 1) dropped = false, want true")
	}
	if len(sockets) != 1 || sockets[0].Inode != 300001 {
		t.Errorf("parseNetFile(limit 1) = %+v, want solo el primer socket", sockets)
	}

	sockets, dropped = parseNetFile([]byte(data), domain.ProtocolTCP, 2)
	if dropped {
		t.Error("parseNetFile(limit 2) dropped = true, want false")
	}
	if len(sockets) != 2 {
		t.Errorf("parseNetFile(limit 2) = %d sockets, want 2", len(sockets))
	}
}

func TestParseNetFileEmpty(t *testing.T) {
	for _, data := range []string{"", netHeader, "\n", "basura"} {
		sockets, dropped := parseNetFile([]byte(data), domain.ProtocolUDP, domain.MaxSockets)
		if len(sockets) != 0 || dropped {
			t.Errorf("parseNetFile(%q) = %d sockets, dropped %v, want 0, false", data, len(sockets), dropped)
		}
	}
}
