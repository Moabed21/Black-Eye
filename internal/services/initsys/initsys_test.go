package initsys

import (
	"testing"
)

func TestInitBackendFactory(t *testing.T) {
	sd := NewSystemd()
	if sd.Name() != "systemd" {
		t.Errorf("expected Systemd backend name systemd, got %s", sd.Name())
	}

	orc := NewOpenRC()
	if orc.Name() != "openrc" {
		t.Errorf("expected OpenRC backend name openrc, got %s", orc.Name())
	}

	sysv := NewSysVinit()
	if sysv.Name() != "sysvinit" {
		t.Errorf("expected SysVinit backend name sysvinit, got %s", sysv.Name())
	}
}
