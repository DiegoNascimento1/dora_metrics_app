package identities

import (
	"testing"

	"github.com/google/uuid"
)

func id(kind, username, email string) Identity {
	return Identity{
		ID:               uuid.New(),
		Kind:             kind,
		ExternalUsername: username,
		ExternalEmail:    email,
	}
}

func TestMatch_EmailExact(t *testing.T) {
	gl := id("gitlab", "alice_dev", "alice@acme.com")
	jr := id("jira", "5f8a...", "ALICE@acme.com") // case difere
	got := Match([]Identity{gl, jr})

	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(got))
	}
	if got[0].Reason != "email_exact" {
		t.Errorf("reason = %s, want email_exact", got[0].Reason)
	}
	if got[0].Score != 1.0 {
		t.Errorf("score = %v, want 1.0", got[0].Score)
	}
}

func TestMatch_UsernameExactWithoutEmail(t *testing.T) {
	gl := id("gitlab", "alice", "")
	jr := id("jira", "ALICE", "") // case difere
	got := Match([]Identity{gl, jr})

	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(got))
	}
	if got[0].Reason != "username_exact" {
		t.Errorf("reason = %s, want username_exact", got[0].Reason)
	}
	if got[0].Score != 0.7 {
		t.Errorf("score = %v, want 0.7", got[0].Score)
	}
}

func TestMatch_EmailWinsOverUsername(t *testing.T) {
	// Mesmo email > mesmo username quando ambos batem.
	gl := id("gitlab", "alice", "alice@acme.com")
	jr := id("jira", "alice", "alice@acme.com")
	got := Match([]Identity{gl, jr})

	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(got))
	}
	if got[0].Reason != "email_exact" {
		t.Errorf("reason = %s, want email_exact", got[0].Reason)
	}
}

func TestMatch_NoCrossKindNoSuggestion(t *testing.T) {
	// Duas identidades GitLab idênticas — não deve sugerir (mesma kind).
	a := id("gitlab", "alice", "alice@acme.com")
	b := id("gitlab", "alice", "alice@acme.com")
	got := Match([]Identity{a, b})

	if len(got) != 0 {
		t.Fatalf("expected 0 suggestions (same kind), got %d", len(got))
	}
}

func TestMatch_NoMatch(t *testing.T) {
	gl := id("gitlab", "alice", "alice@acme.com")
	jr := id("jira", "bob", "bob@acme.com")
	got := Match([]Identity{gl, jr})
	if len(got) != 0 {
		t.Errorf("expected 0 suggestions, got %d", len(got))
	}
}

func TestMatch_SortedByScoreDesc(t *testing.T) {
	gl1 := id("gitlab", "alice", "alice@acme.com")
	jr1 := id("jira", "alice", "alice@acme.com")
	gl2 := id("gitlab", "bob", "")
	jr2 := id("jira", "bob", "")
	got := Match([]Identity{gl1, jr1, gl2, jr2})

	if len(got) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(got))
	}
	if got[0].Score < got[1].Score {
		t.Errorf("not sorted by score desc: %v", got)
	}
	if got[0].Reason != "email_exact" || got[1].Reason != "username_exact" {
		t.Errorf("unexpected order: %v", got)
	}
}

func TestMatch_IgnoresEmptyEmail(t *testing.T) {
	// Emails vazios não devem fazer match acidental.
	gl := id("gitlab", "alice", "")
	jr := id("jira", "bob", "")
	got := Match([]Identity{gl, jr})
	if len(got) != 0 {
		t.Errorf("expected 0 suggestions for empty emails, got %d", len(got))
	}
}

func TestPairKey_StableAcrossOrder(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	k1 := pairKey(a, b)
	k2 := pairKey(b, a)
	if k1 != k2 {
		t.Errorf("pairKey not order-independent: %s vs %s", k1, k2)
	}
}
