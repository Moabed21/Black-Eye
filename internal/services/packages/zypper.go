package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type Zypper struct {
	bin string
}

func NewZypper(bin string) *Zypper {
	if bin == "" {
		bin = "/usr/bin/zypper"
	}
	return &Zypper{bin: bin}
}

func (z *Zypper) Name() string { return "zypper" }

func (z *Zypper) ListInstalled() ([]Package, error) {
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

func (z *Zypper) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command(z.bin, "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) >= 4 {
			name := strings.TrimSpace(parts[1])
			desc := strings.TrimSpace(parts[3])
			if name != "Name" {
				pkgs = append(pkgs, Package{
					Name:        name,
					Description: desc,
					Status:      "available",
				})
			}
		}
	}
	if len(pkgs) > 100 {
		pkgs = pkgs[:100]
	}
	return pkgs, nil
}

func (z *Zypper) Install(names ...string) (string, error) {
	args := append([]string{"--non-interactive", "in"}, names...)
	cmd := exec.Command(z.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (z *Zypper) Remove(names ...string) (string, error) {
	args := append([]string{"--non-interactive", "rm"}, names...)
	cmd := exec.Command(z.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (z *Zypper) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(z.bin, "lu")
	out, _ := cmd.Output()

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) >= 5 {
			name := strings.TrimSpace(parts[2])
			ver := strings.TrimSpace(parts[4])
			if name != "Name" {
				pkgs = append(pkgs, Package{
					Name:    name,
					Version: ver,
					Status:  "upgradable",
				})
			}
		}
	}
	return pkgs, nil
}

func (z *Zypper) UpgradeAll() (string, error) {
	cmd := exec.Command(z.bin, "--non-interactive", "update")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
