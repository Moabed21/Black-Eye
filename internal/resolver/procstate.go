package resolver

// procStates maps the single-char process state from /proc/<pid>/status
// to "CHAR (description)" format.
var procStates = map[byte]string{
	'R': "R (Running)",
	'S': "S (Sleeping)",
	'D': "D (Waiting for I/O)",
	'Z': "Z (Zombie — orphaned)",
	'T': "T (Stopped)",
	'I': "I (Idle)",
	'X': "X (Dead)",
	't': "t (Tracing Stop)",
	'W': "W (Paging)",
	'P': "P (Parked)",
}

// ProcState resolves a single process state character to a human-readable label.
// Returns "? (Unknown)" for unrecognised characters.
func ProcState(state byte) string {
	if label, ok := procStates[state]; ok {
		return label
	}
	return string(state) + " (Unknown)"
}

// ProcStateStr is a string-argument convenience wrapper over ProcState.
func ProcStateStr(state string) string {
	if len(state) == 0 {
		return "? (Unknown)"
	}
	return ProcState(state[0])
}
