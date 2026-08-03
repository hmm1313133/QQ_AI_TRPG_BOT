package world

import "fmt"

// ConsequenceEngine derives secondary state changes from applied events.
// Rule-based and deterministic: no LLM calls, fully testable and replayable.
type ConsequenceEngine interface {
	Propagate(ws *WorldState, ev WorldEvent)
}

// Rule tuning constants.
const (
	allyTrustThreshold = 20  // edges with Trust above this count as close bonds
	deathMoodValence   = -40 // mood shift for bonded NPCs on loss
	deathMoodArousal   = 20
	deathTrustDelta    = -50 // trust shift toward the actor
	deathFearDelta     = 30
	hostileArousal     = 30
	friendlyTrustDelta = 10
	encounterArousal   = 20
)

type ruleConsequence struct{}

type noopConsequence struct{}

// NewRuleConsequence returns the default rule-based engine.
func NewRuleConsequence() ConsequenceEngine {
	return ruleConsequence{}
}

// NewNoopConsequence returns a disabled engine (for tests or fallback).
func NewNoopConsequence() ConsequenceEngine {
	return noopConsequence{}
}

func (noopConsequence) Propagate(ws *WorldState, ev WorldEvent) {}

func (ruleConsequence) Propagate(ws *WorldState, ev WorldEvent) {
	switch ev.Type {
	case "npc_disposition":
		propagateDisposition(ws, ev)
	case "event_triggered":
		propagateEventTriggered(ws, ev)
	}
}

// propagateDisposition handles ripple effects of attitude changes.
func propagateDisposition(ws *WorldState, ev WorldEvent) {
	target := findCharacter(ws, ev.Target)
	if target == nil {
		return
	}
	actor := ev.Actor
	if actor == "" {
		actor = "player"
	}

	switch ev.Value {
	case "dead":
		// Bonded NPCs (close relation edges pointing to the target) react:
		// their mood drops and their trust toward the actor falls.
		for i := range ws.Relations {
			edge := &ws.Relations[i]
			if edge.To != target.Name || edge.From == actor {
				continue
			}
			if edge.Trust <= allyTrustThreshold && !hasStringTag(edge.Tags, "ally") && !hasStringTag(edge.Tags, "family") {
				continue
			}
			if npc := findCharacter(ws, edge.From); npc != nil && npc.Alive {
				applyMoodDelta(&npc.Mood,
					fmt.Sprintf("valence=%d,arousal=+%d,tag=grieving", deathMoodValence, deathMoodArousal),
					ws.Clock.WorldTime)
			}
			rel := ws.GetRelation(edge.From, actor)
			rel.Trust = clamp(rel.Trust+deathTrustDelta, -100, 100)
			rel.Fear = clamp(rel.Fear+deathFearDelta, -100, 100)
			appendDerivedNote(ws, ev,
				fmt.Sprintf("%s now distrusts %s because of what happened to %s", edge.From, actor, target.Name))
		}

	case "hostile":
		applyMoodDelta(&target.Mood,
			fmt.Sprintf("arousal=+%d,tag=agitated", hostileArousal),
			ws.Clock.WorldTime)

	case "friendly":
		rel := ws.GetRelation(target.Name, actor)
		rel.Trust = clamp(rel.Trust+friendlyTrustDelta, -100, 100)
	}
}

// propagateEventTriggered raises alertness of present NPCs on encounters.
func propagateEventTriggered(ws *WorldState, ev WorldEvent) {
	for _, sched := range ws.EventQueue {
		if sched.ID != ev.Target && sched.Description != ev.Target {
			continue
		}
		if sched.Type != "encounter" {
			return
		}
		for _, npc := range ws.Characters {
			if !npc.Alive {
				continue
			}
			applyMoodDelta(&npc.Mood,
				fmt.Sprintf("arousal=+%d,tag=alert", encounterArousal),
				ws.Clock.WorldTime)
		}
		return
	}
}

// appendDerivedNote records a derived effect in the event log for traceability.
// Notes are appended directly (not via ApplyEvent) to avoid recursion.
func appendDerivedNote(ws *WorldState, src WorldEvent, msg string) {
	ws.EventLog = append(ws.EventLog, WorldEvent{
		Type:     "note",
		Actor:    "consequence",
		Target:   src.Target,
		Value:    msg,
		CausedBy: src.Type + ":" + src.Target,
		Round:    ws.RoundCount,
		Time:     ws.Clock.WorldTime,
	})
}

// hasStringTag checks a string slice for a tag.
func hasStringTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
