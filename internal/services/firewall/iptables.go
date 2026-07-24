package firewall

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type IPTables struct {
	bin string
}

func NewIPTables(bin string) *IPTables {
	if bin == "" {
		bin = "/usr/sbin/iptables"
	}
	return &IPTables{bin: bin}
}

func (i *IPTables) Name() string { return "iptables" }

func (i *IPTables) IsEnabled() (bool, error) {
	cmd := exec.Command(i.bin, "-L", "-n")
	err := cmd.Run()
	return err == nil, nil
}

func (i *IPTables) ListRules() ([]Rule, error) {
	cmd := exec.Command(i.bin, "-S")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("iptables -S: %w", err)
	}

	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	lineNo := 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "-A ") {
			continue
		}

		fields := strings.Fields(line)
		chain := "INPUT"
		action := "ACCEPT"
		proto := "all"
		port := "any"

		for j := 0; j < len(fields); j++ {
			switch fields[j] {
			case "-A":
				if j+1 < len(fields) {
					chain = fields[j+1]
				}
			case "-j":
				if j+1 < len(fields) {
					action = fields[j+1]
				}
			case "-p":
				if j+1 < len(fields) {
					proto = fields[j+1]
				}
			case "--dport":
				if j+1 < len(fields) {
					port = fields[j+1]
				}
			}
		}

		rules = append(rules, Rule{
			ID:       fmt.Sprintf("%d", lineNo),
			Chain:    chain,
			Table:    "filter",
			Action:   action,
			Protocol: proto,
			Port:     port,
		})
		lineNo++
	}
	return rules, nil
}

func (i *IPTables) AddRule(r Rule) error {
	chain := r.Chain
	if chain == "" {
		chain = "INPUT"
	}
	action := r.Action
	if action == "" {
		action = "ACCEPT"
	}

	args := []string{"-A", chain}
	if r.Protocol != "" && r.Protocol != "all" {
		args = append(args, "-p", r.Protocol)
	}
	if r.Port != "" && r.Port != "any" {
		args = append(args, "--dport", r.Port)
	}
	args = append(args, "-j", action)

	cmd := exec.Command(i.bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables add: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (i *IPTables) DeleteRule(id string) error {
	cmd := exec.Command(i.bin, "-D", "INPUT", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables delete: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (i *IPTables) Enable() error  { return nil }
func (i *IPTables) Disable() error { return nil }
