package world

import "testing"

func newConsequenceTestState() *WorldState {
	ws := NewWorldState("w-test", ModeTRPG)
	ws.Characters["Alice"] = &CharacterState{Name: "Alice", Kind: "npc", Alive: true, Disposition: "neutral"}
	ws.Characters["Bob"] = &CharacterState{Name: "Bob", Kind: "npc", Alive: true, Disposition: "neutral"}
	ws.Characters["Carol"] = &CharacterState{Name: "Carol", Kind: "npc", Alive: true, Disposition: "neutral"}
	// Bob is a close ally of Alice; Carol barely knows her.
	ws.Relations = []RelationEdge{
		{From: "Bob", To: "Alice", Trust: 60, Tags: []string{"ally"}},
		{From: "Carol", To: "Alice", Trust: 5},
	}
	return ws
}

func TestConsequence_DeathRipple(t *testing.T) {
	e := NewEngine(nil)
	ws := newConsequenceTestState()

	ok, err := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Actor: "player", Target: "Alice", Value: "dead"})
	if !ok || err != nil {
		t.Fatalf("expected event to apply: ok=%v err=%v", ok, err)
	}

	// Bob (ally) should distrust the player now.
	rel := ws.GetRelation("Bob", "player")
	if rel.Trust != deathTrustDelta {
		t.Fatalf("expected Bob->player trust %d, got %d", deathTrustDelta, rel.Trust)
	}
	if rel.Fear != deathFearDelta {
		t.Fatalf("expected Bob->player fear %d, got %d", deathFearDelta, rel.Fear)
	}
	// Bob should be grieving.
	bob := ws.Characters["Bob"]
	if bob.Mood.Valence != deathMoodValence {
		t.Fatalf("expected Bob valence %d, got %d", deathMoodValence, bob.Mood.Valence)
	}
	if !hasStringTag(bob.Mood.Tags, "grieving") {
		t.Fatalf("expected Bob to carry grieving tag, got %v", bob.Mood.Tags)
	}

	// Carol (weak bond) should be unaffected.
	carol := ws.Characters["Carol"]
	if carol.Mood.Valence != 0 {
		t.Fatalf("expected Carol unaffected, got valence %d", carol.Mood.Valence)
	}
	carolRel := ws.GetRelation("Carol", "player")
	if carolRel.Trust != 0 {
		t.Fatalf("expected Carol->player trust 0, got %d", carolRel.Trust)
	}

	// A derived note should be logged for traceability.
	foundNote := false
	for _, ev := range ws.EventLog {
		if ev.Type == "note" && ev.Actor == "consequence" {
			foundNote = true
			if ev.CausedBy != "npc_disposition:Alice" {
				t.Fatalf("unexpected CausedBy: %s", ev.CausedBy)
			}
		}
	}
	if !foundNote {
		t.Fatal("expected a derived note in the event log")
	}
}

func TestConsequence_HostileRaisesArousal(t *testing.T) {
	e := NewEngine(nil)
	ws := newConsequenceTestState()

	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: "Bob", Value: "hostile"})
	if !ok {
		t.Fatal("expected disposition change to apply")
	}
	bob := ws.Characters["Bob"]
	if bob.Mood.Arousal != hostileArousal {
		t.Fatalf("expected Bob arousal %d, got %d", hostileArousal, bob.Mood.Arousal)
	}
	if !hasStringTag(bob.Mood.Tags, "agitated") {
		t.Fatalf("expected agitated tag, got %v", bob.Mood.Tags)
	}
}

func TestConsequence_FriendlyBuildsTrust(t *testing.T) {
	e := NewEngine(nil)
	ws := newConsequenceTestState()

	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Actor: "player", Target: "Carol", Value: "friendly"})
	if !ok {
		t.Fatal("expected disposition change to apply")
	}
	rel := ws.GetRelation("Carol", "player")
	if rel.Trust != friendlyTrustDelta {
		t.Fatalf("expected Carol->player trust %d, got %d", friendlyTrustDelta, rel.Trust)
	}
}

func TestConsequence_EncounterRaisesAlert(t *testing.T) {
	e := NewEngine(nil)
	ws := newConsequenceTestState()
	ws.EventQueue = []ScheduledEvent{
		{ID: "ev1", Description: "a wild beast attacks", Type: "encounter"},
	}

	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "event_triggered", Target: "ev1"})
	if !ok {
		t.Fatal("expected event trigger to apply")
	}
	for name, npc := range ws.Characters {
		if npc.Mood.Arousal != encounterArousal {
			t.Fatalf("expected %s arousal %d, got %d", name, encounterArousal, npc.Mood.Arousal)
		}
		if !hasStringTag(npc.Mood.Tags, "alert") {
			t.Fatalf("expected %s alert tag, got %v", name, npc.Mood.Tags)
		}
	}
}

func TestConsequence_LockedTargetSkipped(t *testing.T) {
	e := NewEngine(nil)
	ws := newConsequenceTestState()

	// Alice dies once (locks), then a second death event is rejected,
	// so no double propagation.
	e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: "Alice", Value: "dead"})
	before := len(ws.EventLog)
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: "Alice", Value: "dead"})
	if ok {
		t.Fatal("second death event should be rejected by the lock")
	}
	if len(ws.EventLog) != before {
		t.Fatal("rejected event must not add log entries")
	}
}
