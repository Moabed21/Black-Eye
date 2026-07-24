package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type DNF struct {
	bin string
}

func NewDNF(bin string) *DNF {
	if bin == "" {
		bin = "/usr/bin/dnf"
	}
	return &DNF{bin: bin}
}

func (d *DNF) Name() string { return "dnf" }

func (d *DNF) ListInstalled() ([]Package, error) {
	cmd := exec.Command("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{SUMMARY}\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rpm -qa: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) >= 3 {
			desc := ""
			if len(parts) >= 4 {
				desc = parts[3]
			}
			pkgs = append(pkgs, Package{
				Name:        parts[0],
				Version:     parts[1],
				Arch:        parts[2],
				Description: desc,
				Status:      "installed",
			})
		}
	}
	return pkgs, nil
}

func (d *DNF) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command(d.bin, "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dnf search: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, " : ") {
			parts := strings.SplitN(line, " : ", 2)
			name := strings.Fields(parts[0])[0]
			pkgs = append(pkgs, Package{
				Name:        name,
				Description: parts[1],
				Status:      "available",
			})
		}
	}
	if len(pkgs) > 100 {
		pkgs = pkgs[:100]
	}
	return pkgs, nil
}

func (d *DNF) Install(names ...string) (string, error) {
	args := append([]string{"install", "-y"}, names...)
	cmd := exec.Command(d.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (d *DNF) Remove(names ...string) (string, error) {
	args := append([]string{"remove", "-y"}, names...)
	cmd := exec.Command(d.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (d *DNF) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(d.bin, "check-update")
	out, _ := cmd.Output()

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 2 && !strings.HasPrefix(line, " ") {
			pkgs = append(pkgs, Package{
				Name:    fields[0],
				Version: fields[1],
				Status:  "upgradable",
			})
		}
	}
	return pkgs, nil
}

func (d *DNF) UpgradeAll() (string, error) {
	cmd := exec.Command(d.bin, "update", "-y")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
