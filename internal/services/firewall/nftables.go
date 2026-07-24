package firewall

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type NFTables struct {
	bin string
}

func NewNFTables(bin string) *NFTables {
	if bin == "" {
		bin = "/usr/sbin/nft"
	}
	return &NFTables{bin: bin}
}

func (n *NFTables) Name() string { return "nftables" }

func (n *NFTables) IsEnabled() (bool, error) {
	cmd := exec.Command(n.bin, "list", "ruleset")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

func (n *NFTables) ListRules() ([]Rule, error) {
	cmd := exec.Command(n.bin, "-j", "list", "ruleset")
	out, err := cmd.Output()
	if err != nil {
		// Fallback without -j
		return n.listRulesPlain()
	}

	var root struct {
		Nftables []map[string]interface{} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		return n.listRulesPlain()
	}

	var rules []Rule
	for _, item := range root.Nftables {
		if ruleObj, ok := item["rule"].(map[string]interface{}); ok {
			chain, _ := ruleObj["chain"].(string)
			table, _ := ruleObj["table"].(string)
			handleVal, _ := ruleObj["handle"].(float64)

			r := Rule{
				ID:    fmt.Sprintf("%.0f", handleVal),
				Chain: chain,
				Table: table,
			}

			// Parse expr array
			if exprs, ok := ruleObj["expr"].([]interface{}); ok {
				for _, exp := range exprs {
					if expMap, ok := exp.(map[string]interface{}); ok {
						if match, ok := expMap["match"].(map[string]interface{}); ok {
							if left, ok := match["left"].(map[string]interface{}); ok {
								if payload, ok := left["payload"].(map[string]interface{}); ok {
									if field, ok := payload["field"].(string); ok {
										if field == "dport" {
											if rightVal, ok := match["right"].(float64); ok {
												r.Port = strconv.Itoa(int(rightVal))
											}
										}
									}
								}
							}
						}
						if verdict, ok := expMap["accept"]; ok && verdict != nil {
							r.Action = "ACCEPT"
						}
						if verdict, ok := expMap["drop"]; ok && verdict != nil {
							r.Action = "DROP"
						}
						if verdict, ok := expMap["reject"]; ok && verdict != nil {
							r.Action = "REJECT"
						}
					}
				}
			}

			if r.Action == "" {
				r.Action = "PASS"
			}
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func (n *NFTables) listRulesPlain() ([]Rule, error) {
	cmd := exec.Command(n.bin, "list", "ruleset")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nft list ruleset: %w", err)
	}

	var rules []Rule
	lines := strings.Split(string(out), "\n")
	var curChain, curTable string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "table ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				curTable = fields[2]
			}
		} else if strings.HasPrefix(line, "chain ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				curChain = fields[1]
			}
		} else if strings.Contains(line, "accept") || strings.Contains(line, "drop") || strings.Contains(line, "reject") {
			act := "ACCEPT"
			if strings.Contains(line, "drop") {
				act = "DROP"
			} else if strings.Contains(line, "reject") {
				act = "REJECT"
			}
			rules = append(rules, Rule{
				ID:     fmt.Sprintf("%d", len(rules)+1),
				Chain:  curChain,
				Table:  curTable,
				Action: act,
				Port:   "any",
			})
		}
	}
	return rules, nil
}

func (n *NFTables) AddRule(r Rule) error {
	chain := r.Chain
	if chain == "" {
		chain = "input"
	}
	table := r.Table
	if table == "" {
		table = "filter"
	}
	action := strings.ToLower(r.Action)
	if action == "" {
		action = "accept"
	}

	cmdStr := fmt.Sprintf("add rule inet %s %s dport %s %s", table, chain, r.Port, action)
	if r.Port == "" || r.Port == "any" {
		cmdStr = fmt.Sprintf("add rule inet %s %s %s", table, chain, action)
	}

	cmd := exec.Command(n.bin, strings.Fields(cmdStr)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft add rule: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (n *NFTables) DeleteRule(id string) error {
	cmd := exec.Command(n.bin, "delete", "rule", "handle", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft delete rule: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (n *NFTables) Enable() error  { return nil }
func (n *NFTables) Disable() error { return nil }
