package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type APT struct {
	bin string
}

func NewAPT(bin string) *APT {
	if bin == "" {
		bin = "/usr/bin/apt"
	}
	return &APT{bin: bin}
}

func (a *APT) Name() string { return "apt" }

func (a *APT) ListInstalled() ([]Package, error) {
	cmd := exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\t${Architecture}\t${Summary}\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dpkg-query: %w", err)
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

func (a *APT) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command("apt-cache", "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("apt-cache search: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " - ", 2)
		if len(parts) == 2 {
			pkgs = append(pkgs, Package{
				Name:        parts[0],
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

func (a *APT) Install(names ...string) (string, error) {
	args := append([]string{"install", "-y"}, names...)
	cmd := exec.Command(a.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *APT) Remove(names ...string) (string, error) {
	args := append([]string{"remove", "-y"}, names...)
	cmd := exec.Command(a.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *APT) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(a.bin, "list", "--upgradable")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Listing") || !strings.Contains(line, "/") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := strings.Split(parts[0], "/")[0]
			pkgs = append(pkgs, Package{
				Name:    name,
				Version: parts[1],
				Status:  "upgradable",
			})
		}
	}
	return pkgs, nil
}

func (a *APT) UpgradeAll() (string, error) {
	cmd := exec.Command(a.bin, "upgrade", "-y")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
