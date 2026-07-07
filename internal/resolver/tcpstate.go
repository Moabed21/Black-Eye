package resolver

import "strings"

// tcpStates maps /proc/net/tcp hex state codes to "CODE (description)".
var tcpStates = map[string]string{
	"01": "ESTABLISHED (Connected)",
	"02": "SYN_SENT (Connecting…)",
	"03": "SYN_RECV (Accepting…)",
	"04": "FIN_WAIT1 (Closing — FIN₁)",
	"05": "FIN_WAIT2 (Closing — FIN₂)",
	"06": "TIME_WAIT (Cooling Down)",
	"07": "CLOSE (Closed)",
	"08": "CLOSE_WAIT (Peer Closed)",
	"09": "LAST_ACK (Finishing)",
	"0A": "LISTEN (Waiting for connections)",
	"0B": "CLOSING (Both Closing)",
}

// TCPState resolves a /proc/net/tcp hex state string to a human-readable label.
// Input is case-insensitive (e.g. "0a" and "0A" both work).
// Returns the raw code if unrecognised.
func TCPState(hexState string) string {
	upper := strings.ToUpper(strings.TrimSpace(hexState))
	if label, ok := tcpStates[upper]; ok {
		return label
	}
	return hexState
}
