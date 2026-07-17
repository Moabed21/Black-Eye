package systemd

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// unitInfo holds raw D-Bus reply data for one systemd unit.
type unitInfo struct {
	name        string
	description string
	loadState   string
	activeState string
	subState    string
}

// listUnitsViaDbus sends a raw D-Bus method call to org.freedesktop.systemd1
// to list all units, without using any external command or cgo library.
//
// D-Bus method: org.freedesktop.systemd1.Manager.ListUnits
// Returns array of: (name, description, load_state, active_state, sub_state, ...)
func listUnitsViaDbus() ([]unitInfo, error) {
	conn, err := net.Dial("unix", dbusSocket)
	if err != nil {
		return nil, fmt.Errorf("dbus dial: %w", err)
	}
	defer conn.Close()

	// D-Bus auth: send AUTH EXTERNAL with our UID (hex-encoded).
	if err := dbusAuth(conn); err != nil {
		return nil, err
	}

	// Send the ListUnits method call.
	msg := buildListUnitsMsg()
	if _, err := conn.Write(msg); err != nil {
		return nil, fmt.Errorf("dbus write: %w", err)
	}

	// Read the reply.
	return readListUnitsReply(conn)
}

// dbusAuth performs D-Bus EXTERNAL authentication (POSIX UID-based).
// Follows the D-Bus spec: send NUL byte, then AUTH EXTERNAL <hex-uid>,
// read OK response, then send BEGIN.
func dbusAuth(conn net.Conn) error {
	// Step 1: Send NUL byte (required by spec as first byte).
	if _, err := conn.Write([]byte{0}); err != nil {
		return fmt.Errorf("dbus auth: NUL byte: %w", err)
	}

	// Step 2: Send AUTH EXTERNAL with hex-encoded UID.
	uid := strconv.Itoa(os.Getuid())
	hexUID := hex.EncodeToString([]byte(uid))
	authCmd := fmt.Sprintf("AUTH EXTERNAL %s\r\n", hexUID)
	if _, err := conn.Write([]byte(authCmd)); err != nil {
		return fmt.Errorf("dbus auth: write: %w", err)
	}

	// Step 3: Read response — expect "OK <guid>".
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("dbus auth: read: %w", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "OK") {
		return fmt.Errorf("dbus auth rejected: %q", resp)
	}

	// Step 4: Send BEGIN to enter message mode.
	if _, err := conn.Write([]byte("BEGIN\r\n")); err != nil {
		return fmt.Errorf("dbus auth: BEGIN: %w", err)
	}

	return nil
}

// buildListUnitsMsg constructs a minimal D-Bus method call message.
func buildListUnitsMsg() []byte {
	const (
		dest   = "org.freedesktop.systemd1"
		path   = "/org/freedesktop/systemd1"
		iface  = "org.freedesktop.systemd1.Manager"
		method = "ListUnits"
	)

	// Build header fields array.
	var hdr []byte
	hdr = appendHeaderField(hdr, 1, 'o', path)   // OBJECT_PATH
	hdr = appendHeaderField(hdr, 2, 's', iface)   // INTERFACE
	hdr = appendHeaderField(hdr, 3, 's', method)  // MEMBER
	hdr = appendHeaderField(hdr, 6, 's', dest)    // DESTINATION

	// Pad header array to 8-byte boundary.
	for len(hdr)%8 != 0 {
		hdr = append(hdr, 0)
	}

	// Fixed message header (12 bytes) + header array length + header array.
	var buf []byte
	buf = append(buf, 'l')                           // little-endian
	buf = append(buf, 1)                             // METHOD_CALL
	buf = append(buf, 0)                             // flags
	buf = append(buf, 1)                             // protocol version
	buf = appendUint32LE(buf, 0)                     // body length
	buf = appendUint32LE(buf, 1)                     // serial
	buf = appendUint32LE(buf, uint32(len(hdr)))      // header array length
	buf = append(buf, hdr...)

	return buf
}

// appendHeaderField adds a D-Bus header field struct: (BYTE code, VARIANT value).
func appendHeaderField(b []byte, code byte, sigChar byte, value string) []byte {
	// Align to 8-byte boundary (struct alignment).
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	b = append(b, code)          // field code
	// Variant: signature length (1 byte) + signature + NUL.
	b = append(b, 1, sigChar, 0) // sig len=1, sig char, NUL
	// Value is string/object path, which requires 4-byte alignment.
	// Since b was 8-byte aligned and we appended exactly 4 bytes, we are perfectly 4-byte aligned.
	b = appendUint32LE(b, uint32(len(value)))
	b = append(b, value...)
	b = append(b, 0) // NUL terminator
	return b
}

func appendUint32LE(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}

// readListUnitsReply reads and parses the D-Bus reply for ListUnits.
func readListUnitsReply(conn net.Conn) ([]unitInfo, error) {
	// Read raw reply bytes — may need multiple reads for large replies.
	var all []byte
	buf := make([]byte, 64*1024) // 64 KiB chunks
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			all = append(all, buf[:n]...)
		}
		if err != nil {
			break
		}
		if n < len(buf) {
			break // likely got everything
		}
	}
	if len(all) < 16 {
		return nil, fmt.Errorf("dbus reply too short (%d bytes)", len(all))
	}

	return extractUnitsFromBytes(all), nil
}

// extractUnitsFromBytes is a heuristic parser that extracts unit names and
// states from raw D-Bus bytes without a full D-Bus library.
func extractUnitsFromBytes(data []byte) []unitInfo {
	var names []string
	i := 0
	for i < len(data)-4 {
		// Try reading a uint32 length prefix.
		length := int(binary.LittleEndian.Uint32(data[i:]))
		// String must have a null terminator and fit in bounds
		if length <= 0 || length > 512 || i+4+length >= len(data) || data[i+4+length] != 0 {
			i++
			continue
		}
		s := string(data[i+4 : i+4+length])

		// Filter to printable ASCII strings of reasonable length.
		printable := true
		for _, c := range s {
			if c < 32 || c > 126 {
				printable = false
				break
			}
		}
		if !printable || len(s) < 3 {
			i++
			continue
		}
		names = append(names, s)
		i += 4 + length + 1 // skip the successfully read string
	}

	// Group consecutive strings into unit records.
	// ListUnits returns (name, desc, load_state, active_state, sub_state, following, path).
	var units []unitInfo
	for j := 0; j+4 < len(names); j++ {
		n := names[j]
		if !strings.HasSuffix(n, ".service") &&
			!strings.HasSuffix(n, ".socket") &&
			!strings.HasSuffix(n, ".timer") &&
			!strings.HasSuffix(n, ".mount") &&
			!strings.HasSuffix(n, ".target") &&
			!strings.HasSuffix(n, ".slice") &&
			!strings.HasSuffix(n, ".scope") {
			continue
		}
		desc := ""
		load := ""
		active := ""
		sub := ""
		if j+1 < len(names) {
			desc = names[j+1]
		}
		if j+2 < len(names) {
			load = names[j+2]
		}
		if j+3 < len(names) {
			active = names[j+3]
		}
		if j+4 < len(names) {
			sub = names[j+4]
		}

		if load == "" || active == "" {
			continue
		}
		units = append(units, unitInfo{
			name:        n,
			description: desc,
			loadState:   load,
			activeState: active,
			subState:    sub,
		})
	}
	return units
}
