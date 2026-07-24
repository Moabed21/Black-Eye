package packages

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

type APK struct {
	bin string
}

func NewAPK(bin string) *APK {
	if bin == "" {
		bin = "/sbin/apk"
	}
	return &APK{bin: bin}
}

func (a *APK) Name() string { return "apk" }

func (a *APK) ListInstalled() ([]Package, error) {
	cmd := exec.Command(a.bin, "list", "-I")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("apk list: %w", err)
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 1 {
			parts := strings.Split(fields[0], "-")
			name := parts[0]
			ver := ""
			if len(parts) > 1 {
				ver = strings.Join(parts[1:], "-")
			}
			pkgs = append(pkgs, Package{
				Name:    name,
				Version: ver,
				Status:  "installed",
			})
		}
	}
	return pkgs, nil
}

func (a *APK) Search(query string) ([]Package, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	cmd := exec.Command(a.bin, "search", query)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			pkgs = append(pkgs, Package{
				Name:   name,
				Status: "available",
			})
		}
	}
	if len(pkgs) > 100 {
		pkgs = pkgs[:100]
	}
	return pkgs, nil
}

func (a *APK) Install(names ...string) (string, error) {
	args := append([]string{"add"}, names...)
	cmd := exec.Command(a.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *APK) Remove(names ...string) (string, error) {
	args := append([]string{"del"}, names...)
	cmd := exec.Command(a.bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (a *APK) ListUpgradable() ([]Package, error) {
	cmd := exec.Command(a.bin, "version", "-l", "<")
	out, _ := cmd.Output()

	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 1 {
			pkgs = append(pkgs, Package{
				Name:   fields[0],
				Status: "upgradable",
			})
		}
	}
	return pkgs, nil
}

func (a *APK) UpgradeAll() (string, error) {
	cmd := exec.Command(a.bin, "upgrade")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
