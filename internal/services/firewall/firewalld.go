package firewall

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type Firewalld struct{}

func NewFirewalld() *Firewalld {
	return &Firewalld{}
}

func (f *Firewalld) Name() string { return "firewalld" }

func (f *Firewalld) IsEnabled() (bool, error) {
	cmd := exec.Command("firewall-cmd", "--state")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "running", nil
}

func (f *Firewalld) ListRules() ([]Rule, error) {
	cmd := exec.Command("firewall-cmd", "--list-all")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("firewall-cmd --list-all: %w", err)
	}

	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	id := 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ports:") {
			portsStr := strings.TrimPrefix(line, "ports:")
			fields := strings.Fields(portsStr)
			for _, p := range fields {
				proto := "tcp"
				port := p
				if parts := strings.Split(p, "/"); len(parts) == 2 {
					port = parts[0]
					proto = parts[1]
				}
				rules = append(rules, Rule{
					ID:       fmt.Sprintf("%d", id),
					Chain:    "IN_public",
					Table:    "firewalld",
					Action:   "ACCEPT",
					Protocol: proto,
					Port:     port,
				})
				id++
			}
		}
	}
	return rules, nil
}

func (f *Firewalld) AddRule(r Rule) error {
	port := r.Port
	if port == "" || port == "any" {
		return fmt.Errorf("firewalld requires a port/protocol e.g. 80/tcp")
	}
	if !strings.Contains(port, "/") {
		proto := r.Protocol
		if proto == "" || proto == "all" {
			proto = "tcp"
		}
		port = port + "/" + proto
	}
	cmd := exec.Command("firewall-cmd", "--add-port="+port, "--permanent")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("firewalld add-port: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	// Reload rules
	exec.Command("firewall-cmd", "--reload").Run()
	return nil
}

func (f *Firewalld) DeleteRule(id string) error {
	rules, err := f.ListRules()
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.ID == id {
			port := r.Port + "/" + r.Protocol
			cmd := exec.Command("firewall-cmd", "--remove-port="+port, "--permanent")
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("firewalld remove-port: %s (%w)", strings.TrimSpace(string(out)), err)
			}
			exec.Command("firewall-cmd", "--reload").Run()
			return nil
		}
	}
	return fmt.Errorf("rule ID %s not found", id)
}

func (f *Firewalld) Enable() error {
	return exec.Command("systemctl", "start", "firewalld").Run()
}

func (f *Firewalld) Disable() error {
	return exec.Command("systemctl", "stop", "firewalld").Run()
}
