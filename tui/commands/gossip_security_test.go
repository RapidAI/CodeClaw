package commands

import (
	"fmt"
	"testing"
)

// ── GossipGuardFn bridge tests (Task 14.2, Req 6.2) ────────────────────────

func TestGossipPublish_BlockedByGuard(t *testing.T) {
	// Validates: Req 6.2 — gossip write commands blocked when guard returns error
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error {
		return fmt.Errorf("Gossip 功能已被管理员禁止")
	}

	err := gossipPublish([]string{"--content", "test", "--category", "news"})
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err.Error() != "Gossip 功能已被管理员禁止" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGossipComment_BlockedByGuard(t *testing.T) {
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error {
		return fmt.Errorf("Gossip 功能已被管理员禁止")
	}

	err := gossipComment([]string{"--post-id", "abc", "--content", "nice"})
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err.Error() != "Gossip 功能已被管理员禁止" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGossipRate_BlockedByGuard(t *testing.T) {
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error {
		return fmt.Errorf("Gossip 功能已被管理员禁止")
	}

	err := gossipRate([]string{"--post-id", "abc", "--rating", "5"})
	if err == nil {
		t.Fatal("expected error when gossip is forbidden")
	}
	if err.Error() != "Gossip 功能已被管理员禁止" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGossipBrowse_NotBlockedByGuard(t *testing.T) {
	// Validates: browse is a read operation and should NOT be blocked
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error {
		return fmt.Errorf("Gossip 功能已被管理员禁止")
	}

	// browse will fail on network (no real server), but should NOT fail on permission
	err := gossipBrowse([]string{})
	if err != nil && err.Error() == "Gossip 功能已被管理员禁止" {
		t.Error("browse should NOT be blocked by gossip guard")
	}
}

func TestGossipComments_NotBlockedByGuard(t *testing.T) {
	// Validates: comments is a read operation and should NOT be blocked
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error {
		return fmt.Errorf("Gossip 功能已被管理员禁止")
	}

	// comments will fail on validation (no post-id), but should NOT fail on permission
	err := gossipComments([]string{"--post-id", "abc"})
	if err != nil && err.Error() == "Gossip 功能已被管理员禁止" {
		t.Error("comments should NOT be blocked by gossip guard")
	}
}

func TestGossipPublish_AllowedWhenGuardNil(t *testing.T) {
	// Validates: when no guard is set, write operations proceed normally
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = nil

	// Will fail on validation or network, but NOT on permission
	err := gossipPublish([]string{"--content", "test", "--category", "news"})
	if err != nil && err.Error() == "Gossip 功能已被管理员禁止" {
		t.Error("publish should be allowed when guard is nil")
	}
}

func TestGossipPublish_AllowedWhenGuardReturnsNil(t *testing.T) {
	// Validates: when guard returns nil, write operations proceed normally
	old := GossipGuardFn
	defer func() { GossipGuardFn = old }()

	GossipGuardFn = func() error { return nil }

	// Will fail on validation or network, but NOT on permission
	err := gossipPublish([]string{"--content", "test", "--category", "news"})
	if err != nil && err.Error() == "Gossip 功能已被管理员禁止" {
		t.Error("publish should be allowed when guard returns nil")
	}
}
