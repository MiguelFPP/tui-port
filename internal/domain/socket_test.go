package domain

import "testing"

func TestProtocolString(t *testing.T) {
	t.Parallel()

	cases := map[Protocol]string{
		ProtocolTCP:  "tcp",
		ProtocolTCP6: "tcp6",
		ProtocolUDP:  "udp",
		ProtocolUDP6: "udp6",
	}
	for protocol, want := range cases {
		if got := protocol.String(); got != want {
			t.Errorf("Protocol(%d).String() = %q, want %q", protocol, got, want)
		}
	}
}

func TestProtocolIsUDP(t *testing.T) {
	t.Parallel()

	if !ProtocolUDP.IsUDP() || !ProtocolUDP6.IsUDP() {
		t.Error("UDP protocols must report IsUDP")
	}
	if ProtocolTCP.IsUDP() || ProtocolTCP6.IsUDP() {
		t.Error("TCP protocols must not report IsUDP")
	}
}

func TestSocketStateString(t *testing.T) {
	t.Parallel()

	cases := map[SocketState]string{
		StateUnknown:     "unknown",
		StateEstablished: "established",
		StateSynSent:     "syn_sent",
		StateSynRecv:     "syn_recv",
		StateFinWait1:    "fin_wait1",
		StateFinWait2:    "fin_wait2",
		StateTimeWait:    "time_wait",
		StateClose:       "close",
		StateCloseWait:   "close_wait",
		StateLastAck:     "last_ack",
		StateListen:      "listen",
		StateClosing:     "closing",
		StateNewSynRecv:  "new_syn_recv",
	}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Errorf("SocketState(%d).String() = %q, want %q", state, got, want)
		}
	}
}

func TestEndpointString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		endpoint Endpoint
		want     string
	}{
		{Endpoint{Addr: "127.0.0.1", Port: 3000}, "127.0.0.1:3000"},
		{Endpoint{Addr: "0.0.0.0", Port: 8080}, "*:8080"},
		{Endpoint{Addr: "::", Port: 5432}, "*:5432"},
		{Endpoint{Addr: "", Port: 0}, "*:0"},
		{Endpoint{Addr: "::1", Port: 8000}, "[::1]:8000"},
	}
	for _, tc := range cases {
		if got := tc.endpoint.String(); got != tc.want {
			t.Errorf("Endpoint.String() = %q, want %q", got, tc.want)
		}
	}
}

func TestEndpointIsWildcard(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []Endpoint{{Addr: ""}, {Addr: "0.0.0.0"}, {Addr: "::"}} {
		if !endpoint.IsWildcard() {
			t.Errorf("%+v must be a wildcard", endpoint)
		}
	}
	for _, endpoint := range []Endpoint{{Addr: "127.0.0.1"}, {Addr: "::1"}} {
		if endpoint.IsWildcard() {
			t.Errorf("%+v must not be a wildcard", endpoint)
		}
	}
}

func TestSocketIDPrefersInode(t *testing.T) {
	t.Parallel()

	socket := Socket{Protocol: ProtocolTCP, Inode: 42}
	if got, want := socket.ID(), "tcp:42"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}

	// A socket without an inode still has a stable identity across refreshes.
	anonymous := Socket{
		Protocol: ProtocolUDP6,
		Local:    Endpoint{Addr: "::", Port: 5353},
		Remote:   Endpoint{Addr: "::1", Port: 6000},
		State:    StateEstablished,
	}
	if got, want := anonymous.ID(), "udp6:*:5353:[::1]:6000:established"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
}

func TestSocketInScope(t *testing.T) {
	t.Parallel()

	listen := Socket{State: StateListen, PID: 10}
	established := Socket{State: StateEstablished, Remote: Endpoint{Addr: "10.0.0.1", Port: 443}}
	boundUDP := Socket{Protocol: ProtocolUDP, State: StateListen}

	cases := []struct {
		name   string
		socket Socket
		scope  Scope
		want   bool
	}{
		{"listen in listen", listen, ScopeListen, true},
		{"listen in connected", listen, ScopeConnected, false},
		{"established in listen", established, ScopeListen, false},
		{"established in connected", established, ScopeConnected, true},
		{"udp bound in listen", boundUDP, ScopeListen, true},
		{"all matches listen", listen, ScopeAll, true},
		{"all matches established", established, ScopeAll, true},
	}
	for _, tc := range cases {
		if got := tc.socket.InScope(tc.scope); got != tc.want {
			t.Errorf("%s: InScope(%s) = %v, want %v", tc.name, tc.scope, got, tc.want)
		}
	}
}

func TestSocketAttributed(t *testing.T) {
	t.Parallel()

	if (Socket{}).Attributed() {
		t.Error("a socket without PID must not be attributed")
	}
	if !(Socket{PID: 7}).Attributed() {
		t.Error("a socket with PID must be attributed")
	}
}
