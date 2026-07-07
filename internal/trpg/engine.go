// Package trpg is the TRPG game core engine.
// It manages game sessions, rulesets, and character bindings.
// The Engine is thread-safe and isolates sessions by sessionID.
package trpg

import (
	"fmt"
	"sync"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset/coc7"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset/dnd5e"
)

// Engine is the TRPG game engine, managing game state and rulesets.
// It is thread-safe; all public methods acquire the internal mutex.
type Engine struct {
	mu             sync.RWMutex
	ruleSets       map[string]ruleset.RuleSet // registered rulesets
	sessions       map[string]*Session        // sessionID -> game session
	defaultRuleSet string                     // default ruleset name for new sessions
}

// NewEngine creates a TRPG engine with CoC7 and DnD5e rulesets registered.
func NewEngine() *Engine {
	e := &Engine{
		ruleSets:       make(map[string]ruleset.RuleSet),
		sessions:       make(map[string]*Session),
		defaultRuleSet: "coc7",
	}
	e.ruleSets["coc7"] = coc7.New()
	e.ruleSets["dnd5e"] = dnd5e.New()
	return e
}

// Session represents an independent TRPG game session (one per QQ group/private chat).
type Session struct {
	ID         string
	RuleSet    ruleset.RuleSet              // active ruleset (coc7/dnd5e)
	Characters map[string]*character.Card   // userID -> active character card (pointer, shared globally)
	Module     string                        // current module name
	State      map[string]interface{}        // arbitrary game state data
}

// GetSession retrieves or creates the game session for the given sessionID.
func (e *Engine) GetSession(sessionID string) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getSessionLocked(sessionID)
}

// getSessionLocked is the lock-free version; caller must hold e.mu.
func (e *Engine) getSessionLocked(sessionID string) *Session {
	if s, ok := e.sessions[sessionID]; ok {
		return s
	}
	s := &Session{
		ID:         sessionID,
		Characters: make(map[string]*character.Card),
		State:      make(map[string]interface{}),
	}
	if e.defaultRuleSet != "" {
		s.RuleSet = e.newRuleSetLocked(e.defaultRuleSet)
	}
	e.sessions[sessionID] = s
	return s
}

// SetRuleSet switches the ruleset for a session.
// A fresh instance is created so each session has independent settings (e.g. house rules).
func (e *Engine) SetRuleSet(sessionID, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.ruleSets[name]; !ok {
		return fmt.Errorf("未知规则集: %s (可用: %v)", name, e.listRuleSetsLocked())
	}
	session := e.getSessionLocked(sessionID)
	session.RuleSet = e.newRuleSetLocked(name)
	return nil
}

// newRuleSetLocked creates a fresh ruleset instance for known types.
// For custom rulesets, returns the registered instance. Caller must hold e.mu.
func (e *Engine) newRuleSetLocked(name string) ruleset.RuleSet {
	switch name {
	case "coc7":
		return coc7.New()
	case "dnd5e":
		return dnd5e.New()
	default:
		return e.ruleSets[name]
	}
}

// GetRuleSet returns the active ruleset for a session (may be nil if not set).
func (e *Engine) GetRuleSet(sessionID string) ruleset.RuleSet {
	session := e.GetSession(sessionID)
	return session.RuleSet
}

// RollDice evaluates a dice expression using the session's ruleset.
func (e *Engine) RollDice(sessionID, expr string) (*dice.RollResult, error) {
	session := e.GetSession(sessionID)
	if session.RuleSet == nil {
		return nil, fmt.Errorf("未设置规则集，请先使用 .set coc 或 .set dnd")
	}
	return session.RuleSet.Roll(expr)
}

// RegisterRuleSet registers a custom ruleset.
func (e *Engine) RegisterRuleSet(name string, rs ruleset.RuleSet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ruleSets[name] = rs
}

// ListRuleSets returns the names of all registered rulesets.
func (e *Engine) ListRuleSets() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.listRuleSetsLocked()
}

func (e *Engine) listRuleSetsLocked() []string {
	names := make([]string, 0, len(e.ruleSets))
	for name := range e.ruleSets {
		names = append(names, name)
	}
	return names
}

// SetDefaultRuleSet sets the default ruleset for new sessions.
func (e *Engine) SetDefaultRuleSet(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.defaultRuleSet = name
}

// GetActiveCharacter returns the character card currently bound to userID in this session.
func (e *Engine) GetActiveCharacter(sessionID, userID string) *character.Card {
	session := e.GetSession(sessionID)
	return session.Characters[userID]
}

// SetActiveCharacter binds a character card to userID in this session.
// The card is a pointer to the global character.Manager's card, so changes
// propagate across all sessions that share the same card.
func (e *Engine) SetActiveCharacter(sessionID, userID string, card *character.Card) {
	session := e.GetSession(sessionID)
	session.Characters[userID] = card
}

// UnsetActiveCharacter removes the character binding for userID in this session.
func (e *Engine) UnsetActiveCharacter(sessionID, userID string) {
	session := e.GetSession(sessionID)
	delete(session.Characters, userID)
}
