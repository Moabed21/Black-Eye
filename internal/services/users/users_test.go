package users_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	usersvc "blackeye/internal/services/users"
)

func TestUsersServiceStartStop(t *testing.T) {
	cfg := config.Defaults()

	svc := usersvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(usersvc.Snapshot)
		if !ok {
			t.Fatal("expected usersvc.Snapshot, got something else")
		}
		if len(snap.Users) == 0 {
			t.Error("expected at least one user entry")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for users snapshot")
	}
}

func TestSudoersRuleValidation(t *testing.T) {
	// Verify input validation for AddSudoRule
	err := usersvc.AddSudoRule("", "", false)
	if err == nil {
		t.Error("expected error for empty user and command")
	}

	// Verify DeleteSudoRule safety check for default /etc/sudoers
	rule := usersvc.SudoRule{Source: "/etc/sudoers"}
	err = usersvc.DeleteSudoRule(rule)
	if err == nil {
		t.Error("expected error when attempting to delete main /etc/sudoers file")
	}
}
