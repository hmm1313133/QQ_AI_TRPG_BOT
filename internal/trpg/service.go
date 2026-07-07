// Package trpg — Service is the unified game operations layer.
// It wraps Engine + character.Manager + SessionManager, providing
// all TRPG game operations as methods. Both command Handlers and
// AI Agent Tools call Service, ensuring single-source-of-truth logic.
package trpg

import (
	"fmt"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset/coc7"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset/dnd5e"
)

// Service is the unified TRPG game service.
// It is thread-safe via the underlying Engine and Manager locks.
type Service struct {
	engine     *Engine
	charMgr    *character.Manager
	sessionMgr *core.SessionManager
}

// NewService creates a unified game service.
func NewService(engine *Engine, charMgr *character.Manager, sessionMgr *core.SessionManager) *Service {
	return &Service{
		engine:     engine,
		charMgr:    charMgr,
		sessionMgr: sessionMgr,
	}
}

// Engine returns the underlying engine (for handlers that need direct access).
func (s *Service) Engine() *Engine { return s.engine }

// CharMgr returns the underlying character manager.
func (s *Service) CharMgr() *character.Manager { return s.charMgr }

// --- generic dice ---

// RollResult is the result of a dice roll.
// Re-exported from dice package for convenience.

// RollDice evaluates a dice expression.
func (s *Service) RollDice(sessionID, expr string) (*dice.RollResult, error) {
	return dice.Roll(expr)
}

// --- ruleset management ---

// SetRuleSet switches the ruleset for a session.
func (s *Service) SetRuleSet(sessionID, name string) error {
	return s.engine.SetRuleSet(sessionID, name)
}

// GetRuleSet returns the active ruleset name for a session.
func (s *Service) GetRuleSet(sessionID string) ruleset.RuleSet {
	return s.engine.GetRuleSet(sessionID)
}

// ListRuleSets returns all registered ruleset names.
func (s *Service) ListRuleSets() []string {
	return s.engine.ListRuleSets()
}

// --- character ---

// GetActiveCharacter returns the character bound to userID in this session.
func (s *Service) GetActiveCharacter(sessionID, userID string) *character.Card {
	return s.engine.GetActiveCharacter(sessionID, userID)
}

// SetActiveCharacter binds a character to userID in this session.
func (s *Service) SetActiveCharacter(sessionID, userID string, card *character.Card) {
	s.engine.SetActiveCharacter(sessionID, userID, card)
}

// UnsetActiveCharacter removes the character binding.
func (s *Service) UnsetActiveCharacter(sessionID, userID string) {
	s.engine.UnsetActiveCharacter(sessionID, userID)
}

// CreateCharacter creates a new character card and binds it.
func (s *Service) CreateCharacter(sessionID, userID, name string) (*character.Card, error) {
	rsName := "coc7"
	if rs := s.engine.GetRuleSet(sessionID); rs != nil {
		rsName = rs.Name()
	}
	card := &character.Card{
		ID:     character.MakeID(userID, name),
		Name:   name,
		Player: userID,
		System: rsName,
		Attrs:  make(map[string]int),
		Skills: make(map[string]int),
		Status: make(map[string]int),
	}
	if err := s.charMgr.Create(card); err != nil {
		return nil, err
	}
	s.engine.SetActiveCharacter(sessionID, userID, card)
	return card, nil
}

// BindCharacter binds an existing character by name.
func (s *Service) BindCharacter(sessionID, userID, name string) (*character.Card, error) {
	card, err := s.charMgr.GetByPlayerAndName(userID, name)
	if err != nil {
		return nil, err
	}
	s.engine.SetActiveCharacter(sessionID, userID, card)
	return card, nil
}

// ListCharacters returns character names for a player.
func (s *Service) ListCharacters(userID string) []string {
	return s.charMgr.ListByPlayer(userID)
}

// DeleteCharacter deletes a character by name.
func (s *Service) DeleteCharacter(sessionID, userID, name string) error {
	if err := s.charMgr.DeleteByPlayerAndName(userID, name); err != nil {
		return err
	}
	if c := s.engine.GetActiveCharacter(sessionID, userID); c != nil && c.Name == name {
		s.engine.UnsetActiveCharacter(sessionID, userID)
	}
	return nil
}

// SetCharacterAttr sets an attribute/skill/status on the active card.
func (s *Service) SetCharacterAttr(sessionID, userID, attr string, value int) error {
	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return fmt.Errorf("未绑定角色卡")
	}
	statusKeys := map[string]bool{"HP": true, "SAN": true, "MP": true,
		"hp": true, "san": true, "mp": true,
		"ds_failures": true, "ds_successes": true}
	if statusKeys[attr] {
		return s.charMgr.SetStatus(card.ID, attr, value)
	}
	return s.charMgr.SetSkill(card.ID, attr, value)
}

// DeleteCharacterAttr deletes an attribute from the active card.
func (s *Service) DeleteCharacterAttr(sessionID, userID, attr string) error {
	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return fmt.Errorf("未绑定角色卡")
	}
	deleted := false
	if _, ok := card.Skills[attr]; ok {
		delete(card.Skills, attr)
		deleted = true
	} else if _, ok := card.Attrs[attr]; ok {
		delete(card.Attrs, attr)
		deleted = true
	} else if _, ok := card.Status[attr]; ok {
		delete(card.Status, attr)
		deleted = true
	}
	if !deleted {
		return fmt.Errorf("未找到属性「%s」", attr)
	}
	return s.charMgr.Update(card)
}

// LookupSkillValue looks up a skill/attr/status value from the active card.
func (s *Service) LookupSkillValue(sessionID, userID, skill string) (int, error) {
	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return 0, fmt.Errorf("未绑定角色卡")
	}
	if v, ok := card.Skills[skill]; ok {
		return v, nil
	}
	if v, ok := card.Attrs[skill]; ok {
		return v, nil
	}
	if v, ok := card.Status[skill]; ok {
		return v, nil
	}
	return 0, fmt.Errorf("角色卡 %s 中没有技能/属性「%s」", card.Name, skill)
}

// GetCharName returns the active character name or "玩家".
func (s *Service) GetCharName(sessionID, userID string) string {
	if card := s.engine.GetActiveCharacter(sessionID, userID); card != nil {
		return card.Name
	}
	return "玩家"
}

// --- CoC7 operations ---

// SkillCheck performs a skill check. For CoC7: roll 1d100 vs value.
// For DnD5e: roll 1d20 + modifier. Value of 0 with non-empty skill
// triggers auto-lookup from character card.
func (s *Service) SkillCheck(sessionID, userID, skill string, value int, opts ruleset.CheckOptions) (*ruleset.CheckResult, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集，请先使用 .set coc 或 .set dnd")
	}

	switch r := rs.(type) {
	case *coc7.CoC7:
		if value == 0 && skill != "" {
			v, err := s.LookupSkillValue(sessionID, userID, skill)
			if err != nil {
				return nil, err
			}
			value = v
		}
		result, err := r.Check(skill, value, opts)
		if err != nil {
			return nil, err
		}
		s.storeResult(sessionID, result.Detail)
		return result, nil

	case *dnd5e.DnD5e:
		modifier := value
		if modifier == 0 && skill != "" {
			v, err := s.LookupSkillValue(sessionID, userID, skill)
			if err != nil {
				return nil, err
			}
			modifier = v
		}
		result, err := r.Check(modifier, opts)
		if err != nil {
			return nil, err
		}
		result.Skill = skill
		s.storeResult(sessionID, result.Detail)
		return result, nil

	default:
		return nil, fmt.Errorf("不支持的规则集: %s", rs.Name())
	}
}

// SANCheck performs a CoC7 SAN check. Reads SAN from character card,
// applies loss, persists new SAN value, and returns the result.
func (s *Service) SANCheck(sessionID, userID, successLoss, failLoss string) (*coc7.SANCheckResult, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return nil, fmt.Errorf("SAN检定仅在 CoC7 规则集下可用")
	}

	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return nil, fmt.Errorf("未绑定角色卡")
	}

	san := 0
	if v, ok := card.Status["SAN"]; ok {
		san = v
	} else if v, ok := card.Attrs["SAN"]; ok {
		san = v
	}

	result, err := c.SANCheck(san, successLoss, failLoss)
	if err != nil {
		return nil, err
	}

	// Persist new SAN to character card
	if err := s.charMgr.SetStatus(card.ID, "SAN", result.NewSAN); err != nil {
		return nil, fmt.Errorf("更新SAN失败: %w", err)
	}

	s.storeResult(sessionID, result.Detail)
	return result, nil
}

// SkillGrowth performs a CoC7 skill growth roll (.en).
// Reads skill value from card, rolls growth, persists new value if grown.
func (s *Service) SkillGrowth(sessionID, userID, skill string) (*coc7.GrowthResult, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return nil, fmt.Errorf("技能成长仅在 CoC7 规则集下可用")
	}

	value, err := s.LookupSkillValue(sessionID, userID, skill)
	if err != nil {
		return nil, err
	}

	result, err := c.SkillGrowth(skill, value)
	if err != nil {
		return nil, err
	}

	if result.Success {
		card := s.engine.GetActiveCharacter(sessionID, userID)
		if card != nil {
			_ = s.charMgr.SetSkill(card.ID, skill, result.NewValue)
		}
	}

	s.storeResult(sessionID, result.Detail)
	return result, nil
}

// GenerateAttrs generates attributes using the active ruleset.
func (s *Service) GenerateAttrs(sessionID string) (map[string]int, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	switch r := rs.(type) {
	case *coc7.CoC7:
		return r.GenerateAttrs()
	case *dnd5e.DnD5e:
		return r.GenerateAttrs()
	default:
		return nil, fmt.Errorf("规则集 %s 不支持属性生成", rs.Name())
	}
}

// OpposedCheck performs a CoC7 opposed check (.rav).
func (s *Service) OpposedCheck(sessionID string, selfVal, oppVal int) (*coc7.OpposedResult, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return nil, fmt.Errorf("对抗检定仅在 CoC7 规则集下可用")
	}
	return c.OpposedCheck(selfVal, oppVal)
}

// RandomMadness returns a random madness symptom (CoC7 only).
func (s *Service) RandomMadness(sessionID string, temporary bool) (string, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return "", fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return "", fmt.Errorf("疯狂症状仅在 CoC7 规则集下可用")
	}
	return c.RandomMadness(temporary), nil
}

// SetHouseRule sets the CoC7 house rule by index.
func (s *Service) SetHouseRule(sessionID string, idx int) error {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return fmt.Errorf("房规设置仅在 CoC7 规则集下可用")
	}
	return c.SetHouseRuleByIndex(idx)
}

// GetHouseRules returns available CoC7 house rules.
func (s *Service) GetHouseRules(sessionID string) (coc7.HouseRule, []coc7.HouseRule, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return coc7.HouseRule{}, nil, fmt.Errorf("未设置规则集")
	}
	c, ok := rs.(*coc7.CoC7)
	if !ok {
		return coc7.HouseRule{}, nil, fmt.Errorf("房规设置仅在 CoC7 规则集下可用")
	}
	return c.HouseRule(), c.HouseRules(), nil
}

// --- DnD5e operations ---

// DeathSave performs a DnD5e death saving throw.
// Reads current counts from card, rolls, persists updated counts.
func (s *Service) DeathSave(sessionID, userID string) (*dnd5e.DeathSaveResult, error) {
	rs := s.engine.GetRuleSet(sessionID)
	if rs == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	d, ok := rs.(*dnd5e.DnD5e)
	if !ok {
		return nil, fmt.Errorf("死亡豁免仅在 DnD5e 规则集下可用")
	}

	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return nil, fmt.Errorf("未绑定角色卡")
	}

	fails, successes := 0, 0
	if v, ok := card.Status["ds_failures"]; ok {
		fails = v
	}
	if v, ok := card.Status["ds_successes"]; ok {
		successes = v
	}

	result, err := d.DeathSave(fails, successes)
	if err != nil {
		return nil, err
	}

	// Persist counts
	if result.Stabilized || result.Dead || result.Revived {
		_ = s.charMgr.SetStatus(card.ID, "ds_failures", 0)
		_ = s.charMgr.SetStatus(card.ID, "ds_successes", 0)
	} else {
		_ = s.charMgr.SetStatus(card.ID, "ds_failures", result.Failures)
		_ = s.charMgr.SetStatus(card.ID, "ds_successes", result.Successes)
	}

	s.storeResult(sessionID, result.Detail)
	return result, nil
}

// LongRest resets death save counts and restores HP (DnD5e).
func (s *Service) LongRest(sessionID, userID string) (int, error) {
	card := s.engine.GetActiveCharacter(sessionID, userID)
	if card == nil {
		return 0, fmt.Errorf("未绑定角色卡")
	}
	_ = s.charMgr.SetStatus(card.ID, "ds_failures", 0)
	_ = s.charMgr.SetStatus(card.ID, "ds_successes", 0)

	maxHP := 0
	if v, ok := card.Attrs["最大HP"]; ok {
		maxHP = v
	}
	if maxHP > 0 {
		_ = s.charMgr.SetStatus(card.ID, "HP", maxHP)
	}
	return maxHP, nil
}

// --- initiative (DnD) ---

// SetInitiative adds or updates a combatant's initiative.
// If value is 0 and modifier > 0, rolls 1d20+modifier.
func (s *Service) SetInitiative(sessionID, name string, value int) int {
	session := s.engine.GetSession(sessionID)
	initList := s.getInitList(session)
	initList[name] = value
	session.State["initiative"] = initList
	return value
}

// RollInitiative rolls 1d20+modifier and stores the result.
func (s *Service) RollInitiative(sessionID, name string, modifier int) (int, error) {
	result, err := dice.Roll(fmt.Sprintf("1d20+%d", modifier))
	if err != nil {
		return 0, err
	}
	s.SetInitiative(sessionID, name, result.Total)
	return result.Total, nil
}

// GetInitList returns the sorted initiative list.
func (s *Service) GetInitList(sessionID string) map[string]int {
	session := s.engine.GetSession(sessionID)
	return s.getInitList(session)
}

// ClearInitiative removes the initiative list.
func (s *Service) ClearInitiative(sessionID string) {
	session := s.engine.GetSession(sessionID)
	delete(session.State, "initiative")
}

func (s *Service) getInitList(session *Session) map[string]int {
	if v, ok := session.State["initiative"]; ok {
		if list, ok := v.(map[string]int); ok {
			return list
		}
	}
	return make(map[string]int)
}

// --- session state helpers ---

// SetDefaultDice sets the default dice sides for a session.
func (s *Service) SetDefaultDice(sessionID string, sides int) {
	session := s.engine.GetSession(sessionID)
	session.State["default_dice_sides"] = sides
}

// GetDefaultDice returns the default dice sides, or 0 if not set.
func (s *Service) GetDefaultDice(sessionID string) int {
	session := s.engine.GetSession(sessionID)
	if v, ok := session.State["default_dice_sides"]; ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

// GetSession returns the trpg session.
func (s *Service) GetSession(sessionID string) *Session {
	return s.engine.GetSession(sessionID)
}

// --- internal ---

// StoreDiceResult saves the last dice roll result in core.Session for the AI to read.
// This is public so DiceHandler can call it after a raw dice roll.
func (s *Service) StoreDiceResult(sessionID, detail string, total int) {
	if s.sessionMgr == nil {
		return
	}
	session := s.sessionMgr.GetSession(sessionID)
	session.Set("last_dice_result", detail)
	session.Set("last_dice_total", total)
}

// storeResult saves the last check/dice result in both core.Session (for AI)
// and is called by all game operations.
func (s *Service) storeResult(sessionID, detail string) {
	if s.sessionMgr == nil {
		return
	}
	session := s.sessionMgr.GetSession(sessionID)
	session.Set("last_check_result", detail)
	session.Set("last_dice_result", detail)
}
