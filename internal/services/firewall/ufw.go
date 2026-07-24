package firewall

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type UFW struct {
	bin string
}

func NewUFW(bin string) *UFW {
	if bin == "" {
		bin = "/usr/sbin/ufw"
	}
	return &UFW{bin: bin}
}

func (u *UFW) Name() string { return "ufw" }

func (u *UFW) IsEnabled() (bool, error) {
	cmd := exec.Command(u.bin, "status")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToLower(string(out)), "status: active"), nil
}

func (u *UFW) ListRules() ([]Rule, error) {
	cmd := exec.Command(u.bin, "status", "numbered")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ufw status: %w", err)
	}

	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "[") {
			continue
		}
		// Example: [ 1] 80/tcp                     ALLOW IN    Anywhere
		endBracket := strings.Index(line, "]")
		if endBracket < 0 {
			continue
		}
		id := strings.TrimSpace(line[1:endBracket])
		rest := strings.TrimSpace(line[endBracket+1:])

		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}

		portProto := fields[0]
		action := fields[1]

		proto := "all"
		port := portProto
		if parts := strings.Split(portProto, "/"); len(parts) == 2 {
			port = parts[0]
			proto = parts[1]
		}

		rules = append(rules, Rule{
			ID:       id,
			Chain:    "INPUT",
			Table:    "ufw",
			Action:   action,
			Protocol: proto,
			Port:     port,
		})
	}
	return rules, nil
}

func (u *UFW) AddRule(r Rule) error {
	action := strings.ToLower(r.Action)
	if action == "" {
		action = "allow"
	}
	port := r.Port
	if port == "" || port == "any" {
		return fmt.Errorf("ufw requires a specific port")
	}

	cmd := exec.Command(u.bin, action, port)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ufw add: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (u *UFW) DeleteRule(id string) error {
	cmd := exec.Command(u.bin, "--force", "delete", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ufw delete: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (u *UFW) Enable() error {
	cmd := exec.Command(u.bin, "--force", "enable")
	return cmd.Run()
}

func (u *UFW) Disable() error {
	cmd := exec.Command(u.bin, "disable")
	return cmd.Run()
}
