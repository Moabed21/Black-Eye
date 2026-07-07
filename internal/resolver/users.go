package resolver

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	usersMu    sync.RWMutex
	uidToUser  map[int]string
	usersReady bool
)

// InitUsers parses /etc/passwd and populates the UID→username cache.
// It is safe to call multiple times; subsequent calls are no-ops.
func InitUsers() error {
	usersMu.Lock()
	defer usersMu.Unlock()

	if usersReady {
		return nil
	}

	m := make(map[int]string)
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return fmt.Errorf("resolver: cannot open /etc/passwd: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, ":", 7)
		if len(fields) < 4 {
			continue
		}
		username := fields[0]
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		if _, exists := m[uid]; !exists {
			m[uid] = username
		}
	}

	uidToUser = m
	usersReady = true
	return scanner.Err()
}

// ByUID resolves a numeric UID to its username.
// Returns "uid:<n>" if the UID is not found in /etc/passwd.
func ByUID(uid int) string {
	usersMu.RLock()
	defer usersMu.RUnlock()
	if name, ok := uidToUser[uid]; ok {
		return name
	}
	return fmt.Sprintf("uid:%d", uid)
}

// ByUIDStr is a string-argument convenience wrapper over ByUID.
func ByUIDStr(uid string) string {
	n, err := strconv.Atoi(strings.TrimSpace(uid))
	if err != nil {
		return uid
	}
	return ByUID(n)
}
