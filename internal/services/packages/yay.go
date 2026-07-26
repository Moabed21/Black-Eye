package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type Yay struct {
	bin string
}

func NewYay(bin string) *Yay {
	if bin == "" {
		bin = "yay"
	}
	return &Yay{bin: bin}
}

func (y *Yay) Name() string { return "yay (AUR)" }

func (y *Yay) ListInstalled() ([]Package, error) {
	cmd := exec.Command(y.bin, "-Qm")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yay -Qm: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			pkgs = append(pkgs, Package{
				Name:        fields[0],
				Version:     fields[1],
				Status:      "installed",
				Category:    CategoryUserInstalled,
				Description: "AUR Package",
			})
		}
	}
	return pkgs, nil
}

func (y *Yay) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command(y.bin, "-Ss", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var currentPkg *Package

	for scanner.Scan() {
		rawLine := stripANSI(scanner.Text())
		if strings.Contains(rawLine, "/") {
			fields := strings.Fields(rawLine)
			if len(fields) >= 2 {
				parts := strings.Split(fields[0], "/")
				name := parts[len(parts)-1]
				pkg := Package{
					Name:        name,
					Version:     fields[1],
					Status:      "available",
					Category:    CategoryUserInstalled,
					Description: "AUR Package",
				}
				pkgs = append(pkgs, pkg)
				currentPkg = &pkgs[len(pkgs)-1]
			}
		} else if currentPkg != nil && strings.HasPrefix(rawLine, "    ") {
			currentPkg.Description = strings.TrimSpace(rawLine)
		}
	}
	if len(pkgs) > 100 {
		pkgs = pkgs[:100]
	}
	return pkgs, nil
}

func stripANSI(str string) string {
	var sb strings.Builder
	inEsc := false
	for i := 0; i < len(str); i++ {
		if str[i] == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (str[i] >= 'A' && str[i] <= 'Z') || (str[i] >= 'a' && str[i] <= 'z') || str[i] == '~' {
				inEsc = false
			}
			continue
		}
		sb.WriteByte(str[i])
	}
	return sb.String()
}

func (y *Yay) Install(names ...string) (string, error) {
	args := append([]string{"-S", "--noconfirm"}, names...)
	cmd := exec.Command(y.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (y *Yay) Remove(names ...string) (string, error) {
	args := append([]string{"-R", "--noconfirm"}, names...)
	cmd := exec.Command(y.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (y *Yay) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(y.bin, "-Qu")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			pkgs = append(pkgs, Package{
				Name:    fields[0],
				Version: fields[1],
				Status:  "upgradable",
			})
		}
	}
	return pkgs, nil
}

func (y *Yay) UpgradeAll() (string, error) {
	cmd := exec.Command(y.bin, "-Syu", "--noconfirm")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
