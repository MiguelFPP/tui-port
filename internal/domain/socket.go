package domain

import (
	"strconv"
	"strings"
)

// Protocol is the transport family of a socket.
type Protocol uint8

const (
	ProtocolTCP Protocol = iota
	ProtocolTCP6
	ProtocolUDP
	ProtocolUDP6
)

func (p Protocol) String() string {
	switch p {
	case ProtocolTCP6:
		return "tcp6"
	case ProtocolUDP:
		return "udp"
	case ProtocolUDP6:
		return "udp6"
	default:
		return "tcp"
	}
}

// IsUDP reports whether the protocol is datagram based.
func (p Protocol) IsUDP() bool {
	return p == ProtocolUDP || p == ProtocolUDP6
}

// SocketState is the kernel state of one socket, with UDP 07 reported as listen.
type SocketState uint8

const (
	StateUnknown SocketState = iota
	StateEstablished
	StateSynSent
	StateSynRecv
	StateFinWait1
	StateFinWait2
	StateTimeWait
	StateClose
	StateCloseWait
	StateLastAck
	StateListen
	StateClosing
	StateNewSynRecv
)

func (s SocketState) String() string {
	switch s {
	case StateEstablished:
		return "established"
	case StateSynSent:
		return "syn_sent"
	case StateSynRecv:
		return "syn_recv"
	case StateFinWait1:
		return "fin_wait1"
	case StateFinWait2:
		return "fin_wait2"
	case StateTimeWait:
		return "time_wait"
	case StateClose:
		return "close"
	case StateCloseWait:
		return "close_wait"
	case StateLastAck:
		return "last_ack"
	case StateListen:
		return "listen"
	case StateClosing:
		return "closing"
	case StateNewSynRecv:
		return "new_syn_recv"
	default:
		return "unknown"
	}
}

// Endpoint is one address and port of a socket.
type Endpoint struct {
	Addr string
	Port uint16
}

// IsWildcard reports whether the address accepts every interface.
func (e Endpoint) IsWildcard() bool {
	switch e.Addr {
	case "", "0.0.0.0", "::":
		return true
	default:
		return false
	}
}

// String renders the endpoint with * for a wildcard and brackets for IPv6.
func (e Endpoint) String() string {
	host := e.Addr
	switch {
	case e.IsWildcard():
		host = "*"
	case strings.Contains(host, ":"):
		host = "[" + host + "]"
	}

	return host + ":" + strconv.Itoa(int(e.Port))
}

// Socket is one row of /proc/net: a transport endpoint of the kernel.
type Socket struct {
	Protocol Protocol
	Local    Endpoint
	Remote   Endpoint
	State    SocketState
	UID      uint32
	Inode    uint64
	PID      int // zero when the owner could not be resolved
}

// ID is the stable identity used to keep the selection across refreshes.
func (s Socket) ID() string {
	if s.Inode != 0 {
		return s.Protocol.String() + ":" + strconv.FormatUint(s.Inode, 10)
	}

	return s.Protocol.String() + ":" + s.Local.String() + ":" + s.Remote.String() + ":" + s.State.String()
}

// IsListening reports a TCP listener or a bound UDP socket.
func (s Socket) IsListening() bool {
	return s.State == StateListen
}

// HasPeer reports whether the socket has a remote endpoint.
func (s Socket) HasPeer() bool {
	return s.Remote.Port != 0 && !s.Remote.IsWildcard()
}

// Attributed reports whether the owning process was resolved.
func (s Socket) Attributed() bool {
	return s.PID > 0
}

// Scope filters the socket list by activity.
type Scope uint8

const (
	ScopeAll Scope = iota
	ScopeListen
	ScopeConnected
)

func (s Scope) String() string {
	switch s {
	case ScopeListen:
		return "listen"
	case ScopeConnected:
		return "connected"
	default:
		return "all"
	}
}

// InScope reports whether the socket belongs to the activity scope.
func (s Socket) InScope(scope Scope) bool {
	switch scope {
	case ScopeListen:
		return s.IsListening()
	case ScopeConnected:
		return !s.IsListening() && s.HasPeer()
	default:
		return true
	}
}
