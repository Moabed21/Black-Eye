package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type Pacman struct {
	bin string
}

func NewPacman(bin string) *Pacman {
	if bin == "" {
		bin = "/usr/bin/pacman"
	}
	return &Pacman{bin: bin}
}

func (p *Pacman) Name() string { return "pacman" }

func (p *Pacman) ListInstalled() ([]Package, error) {
	cmd := exec.Command(p.bin, "-Q")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pacman -Q: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			pkgs = append(pkgs, Package{
				Name:    fields[0],
				Version: fields[1],
				Status:  "installed",
			})
		}
	}
	return pkgs, nil
}

func (p *Pacman) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command(p.bin, "-Ss", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "/") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				parts := strings.Split(fields[0], "/")
				name := parts[len(parts)-1]
				pkgs = append(pkgs, Package{
					Name:    name,
					Version: fields[1],
					Status:  "available",
				})
			}
		}
	}
	if len(pkgs) > 100 {
		pkgs = pkgs[:100]
	}
	return pkgs, nil
}

func (p *Pacman) Install(names ...string) (string, error) {
	args := append([]string{"-S", "--noconfirm"}, names...)
	cmd := exec.Command(p.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *Pacman) Remove(names ...string) (string, error) {
	args := append([]string{"-R", "--noconfirm"}, names...)
	cmd := exec.Command(p.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p *Pacman) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(p.bin, "-Qu")
	out, _ := cmd.Output()

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			pkgs = append(pkgs, Package{
				Name:    fields[0],
				Version: fields[3],
				Status:  "upgradable",
			})
		}
	}
	return pkgs, nil
}

func (p *Pacman) UpgradeAll() (string, error) {
	cmd := exec.Command(p.bin, "-Syu", "--noconfirm")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
