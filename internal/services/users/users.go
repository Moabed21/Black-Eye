// Package users provides user, group, and sudoers inspection and management.
package users

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// UserInfo holds metadata for a single user account.
type UserInfo struct {
	UID      int
	GID      int
	Username string
	FullName string
	Home     string
	Shell    string
	Groups   []string
	Locked   bool
}

// GroupInfo holds metadata for a system group.
type GroupInfo struct {
	GID     int
	Name    string
	Members []string
}

// SudoRule holds a parsed sudoers rule.
type SudoRule struct {
	User    string
	Host    string
	RunAs   string
	Command string
	NoPass  bool
	Source  string // e.g. "/etc/sudoers" or "/etc/sudoers.d/admin"
}

// Snapshot is the event bus payload.
type Snapshot struct {
	Users     []UserInfo
	Groups    []GroupInfo
	SudoRules []SudoRule
	Available bool
	Error     string
	Timestamp time.Time
}

// Service collects user, group, and sudoers status.
type Service struct {
	interval time.Duration
	out      chan interface{}
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	return &Service{
		interval: 10 * time.Second,
		out:      make(chan interface{}, 4),
	}
}

func (s *Service) Name() string { return "User & Privilege Collector" }
func (s *Service) Topic() string { return "users" }
func (s *Service) Output() <-chan interface{} { return s.out }
func (s *Service) Health() services.HealthStatus {
	return services.HealthStatus{State: services.HealthOK}
}
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Reload(cfg config.Config) {}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial snapshot
	select {
	case s.out <- s.collect():
	default:
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			select {
			case s.out <- s.collect():
			default:
			}
		}
	}
}

func (s *Service) collect() Snapshot {
	users, err := parsePasswd()
	if err != nil {
		return Snapshot{Available: false, Error: err.Error(), Timestamp: time.Now()}
	}

	groups, _ := parseGroups()
	sudoRules, _ := parseSudoers()

	// Enrich users with group names
	groupMap := make(map[int]string)
	for _, g := range groups {
		groupMap[g.GID] = g.Name
	}
	for i := range users {
		if gName, ok := groupMap[users[i].GID]; ok {
			users[i].Groups = append(users[i].Groups, gName)
		}
	}

	return Snapshot{
		Users:     users,
		Groups:    groups,
		SudoRules: sudoRules,
		Available: true,
		Timestamp: time.Now(),
	}
}

func parsePasswd() ([]UserInfo, error) {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, fmt.Errorf("open /etc/passwd: %w", err)
	}
	defer f.Close()

	var users []UserInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 7 {
			uid, _ := strconv.Atoi(parts[2])
			gid, _ := strconv.Atoi(parts[3])
			users = append(users, UserInfo{
				Username: parts[0],
				UID:      uid,
				GID:      gid,
				FullName: parts[4],
				Home:     parts[5],
				Shell:    parts[6],
			})
		}
	}
	return users, nil
}

func parseGroups() ([]GroupInfo, error) {
	f, err := os.Open("/etc/group")
	if err != nil {
		return nil, fmt.Errorf("open /etc/group: %w", err)
	}
	defer f.Close()

	var groups []GroupInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			gid, _ := strconv.Atoi(parts[2])
			var members []string
			if len(parts) >= 4 && parts[3] != "" {
				members = strings.Split(parts[3], ",")
			}
			groups = append(groups, GroupInfo{
				Name:    parts[0],
				GID:     gid,
				Members: members,
			})
		}
	}
	return groups, nil
}

func parseSudoers() ([]SudoRule, error) {
	var rules []SudoRule
	files := []string{"/etc/sudoers"}
	if matches, err := filepath.Glob("/etc/sudoers.d/*"); err == nil {
		files = append(files, matches...)
	}

	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") || line == "" || strings.HasPrefix(line, "Defaults") {
				continue
			}
			if strings.Contains(line, "ALL=") || strings.Contains(line, "=") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					rules = append(rules, SudoRule{
						User:    parts[0],
						Host:    "ALL",
						RunAs:   "root",
						Command: strings.Join(parts[1:], " "),
						NoPass:  strings.Contains(line, "NOPASSWD"),
						Source:  filepath.Base(file),
					})
				}
			}
		}
		f.Close()
	}
	return rules, nil
}

// User mutation helper methods
func AddUser(username string) error {
	cmd := exec.Command("useradd", "-m", username)
	if _, err := exec.LookPath("useradd"); err != nil {
		cmd = exec.Command("adduser", "-D", username) // Alpine fallback
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("useradd %s: %s (%w)", username, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func DeleteUser(username string) error {
	cmd := exec.Command("userdel", "-r", username)
	if _, err := exec.LookPath("userdel"); err != nil {
		cmd = exec.Command("deluser", "--remove-home", username) // Alpine fallback
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("userdel %s: %s (%w)", username, strings.TrimSpace(string(out)), err)
	}
	return nil
}
