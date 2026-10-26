package service

import (
	"testing"
	"time"

	"taskmanager/internal/domain"
)

func i64(v int64) *int64                { return &v }
func str(v string) *string              { return &v }
func st(v domain.Status) *domain.Status { return &v }

func TestCanInvite(t *testing.T) {
	tests := []struct {
		name          string
		actor, target domain.Role
		want          bool
	}{
		{"owner invites admin", domain.RoleOwner, domain.RoleAdmin, true},
		{"owner invites member", domain.RoleOwner, domain.RoleMember, true},
		{"owner cannot grant owner", domain.RoleOwner, domain.RoleOwner, false},
		{"admin invites member", domain.RoleAdmin, domain.RoleMember, true},
		{"admin cannot invite admin", domain.RoleAdmin, domain.RoleAdmin, false},
		{"admin cannot grant owner", domain.RoleAdmin, domain.RoleOwner, false},
		{"member cannot invite", domain.RoleMember, domain.RoleMember, false},
		{"unknown role cannot invite", "", domain.RoleMember, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanInvite(tc.actor, tc.target); got != tc.want {
				t.Fatalf("CanInvite(%q,%q)=%v, want %v", tc.actor, tc.target, got, tc.want)
			}
		})
	}
}

func TestCanViewStatsAndChangeRoles(t *testing.T) {
	if !CanViewStats(domain.RoleOwner) || !CanViewStats(domain.RoleAdmin) {
		t.Fatal("owner and admin must see stats")
	}
	if CanViewStats(domain.RoleMember) || CanViewStats("") {
		t.Fatal("member must not see stats")
	}
	if !CanChangeRoles(domain.RoleOwner) || CanChangeRoles(domain.RoleAdmin) || CanChangeRoles(domain.RoleMember) {
		t.Fatal("only owner can change roles")
	}
}

func TestCanEditTask(t *testing.T) {
	const creator, assignee, other = 10, 20, 30
	task := &domain.Task{ID: 1, Title: "T", Description: "D", Status: domain.StatusTodo,
		CreatedBy: creator, AssigneeID: i64(assignee)}

	tests := []struct {
		name    string
		role    domain.Role
		actor   int64
		patch   domain.TaskPatch
		allowed bool
	}{
		{"owner edits any task", domain.RoleOwner, other, domain.TaskPatch{Title: str("x")}, true},
		{"admin edits any task", domain.RoleAdmin, other, domain.TaskPatch{Title: str("x")}, true},
		{"creator edits everything", domain.RoleMember, creator,
			domain.TaskPatch{Title: str("x"), Description: str("y"), Status: st(domain.StatusDone),
				AssigneeID: domain.OptInt64{Set: true, Value: i64(other)}}, true},
		{"assignee changes status", domain.RoleMember, assignee, domain.TaskPatch{Status: st(domain.StatusDone)}, true},
		{"assignee resends unchanged fields", domain.RoleMember, assignee,
			domain.TaskPatch{Title: str("T"), Description: str("D"), Status: st(domain.StatusInProgress),
				AssigneeID: domain.OptInt64{Set: true, Value: i64(assignee)}}, true},
		{"assignee cannot rename", domain.RoleMember, assignee, domain.TaskPatch{Title: str("new")}, false},
		{"assignee cannot edit description", domain.RoleMember, assignee, domain.TaskPatch{Description: str("new")}, false},
		{"assignee cannot reassign", domain.RoleMember, assignee,
			domain.TaskPatch{AssigneeID: domain.OptInt64{Set: true, Value: i64(other)}}, false},
		{"assignee cannot unassign", domain.RoleMember, assignee,
			domain.TaskPatch{AssigneeID: domain.OptInt64{Set: true, Value: nil}}, false},
		{"stranger cannot edit", domain.RoleMember, other, domain.TaskPatch{Status: st(domain.StatusDone)}, false},
		{"non-member cannot edit", "", creator, domain.TaskPatch{Title: str("x")}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CanEditTask(tc.role, tc.actor, task, tc.patch)
			if tc.allowed && err != nil {
				t.Fatalf("expected allowed, got %v", err)
			}
			if !tc.allowed && !errorsIs(err, domain.ErrForbidden) {
				t.Fatalf("expected ErrForbidden, got %v", err)
			}
		})
	}
}

func TestApplyPatchBuildsDiffFromCurrentState(t *testing.T) {
	cur := &domain.Task{ID: 1, Title: "A", Description: "d", Status: domain.StatusTodo, Version: 3}
	next, ch := applyPatch(cur, domain.TaskPatch{Title: str("B"), Status: st(domain.StatusDone)}, timeNow())
	if len(ch) != 3 { // title, status, closed_at
		t.Fatalf("unexpected changes: %#v", ch)
	}
	if ch["title"].Old != "A" || ch["title"].New != "B" {
		t.Fatalf("bad title diff: %#v", ch["title"])
	}
	if next.ClosedAt == nil || next.Status != domain.StatusDone {
		t.Fatal("done must set closed_at")
	}
	if cur.Title != "A" {
		t.Fatal("current state must not be mutated")
	}
	if _, ch := applyPatch(cur, domain.TaskPatch{Title: str("A")}, timeNow()); len(ch) != 0 {
		t.Fatalf("no-op patch must produce no changes, got %#v", ch)
	}
	reopened, ch := applyPatch(next, domain.TaskPatch{Status: st(domain.StatusInProgress)}, timeNow())
	if reopened.ClosedAt != nil || ch["closed_at"].New != nil {
		t.Fatal("leaving done must clear closed_at")
	}
}

func timeNow() time.Time { return time.Now().UTC() }
