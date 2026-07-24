package privilege

import (
	"testing"
)

func TestPrivilege_InitAndGetters(t *testing.T) {
	Init()

	// Ensure calling Init multiple times is idempotent (handled by sync.Once)
	Init()

	_ = IsRoot()
	_ = CanKill()
	_ = HasDockerAccess()
	_ = CanFirewall()
	_ = CanNetConfig()
	_ = CanPackageManage()
	_ = CanManageUsers()
	_ = CanReadShadow()
	_ = CanReadProcIO()
}

func TestPrivilege_CheckCap(t *testing.T) {
	// checkCap for non-existent capability index should safely return false
	res := checkCap(999)
	if res {
		t.Error("expected false for out-of-bounds capability 999")
	}
}
