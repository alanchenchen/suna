package agent

import (
	"fmt"

	"github.com/alanchenchen/suna/internal/skill"
)

// agentSkillStore 将 Skill 的轻量记录更新纳入 Agent 配置锁，避免与 config.set 或外部配置重载并发覆盖。
type agentSkillStore struct {
	agent *Agent
}

func (s agentSkillStore) LoadSkillRecords() map[string]skill.Record {
	if s.agent == nil {
		return nil
	}
	s.agent.configMu.RLock()
	defer s.agent.configMu.RUnlock()
	if s.agent.cfg == nil {
		return nil
	}
	return cloneAgentSkillRecords(s.agent.cfg.Skills)
}

func (s agentSkillStore) SaveSkillRecords(records map[string]skill.Record) error {
	if s.agent == nil {
		return fmt.Errorf("skill record store is not configured")
	}
	s.agent.configMu.Lock()
	defer s.agent.configMu.Unlock()
	if s.agent.cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	cfg := s.agent.cfg.Clone()
	cfg.Skills = cloneAgentSkillRecords(records)
	modTime, err := stageAndCommitConfig(cfg, nil)
	if err != nil {
		return err
	}
	s.agent.cfg = cfg
	s.agent.configModTime = modTime
	return nil
}

func (s agentSkillStore) SaveSkillRecord(name string, record skill.Record) (map[string]skill.Record, error) {
	if s.agent == nil {
		return nil, fmt.Errorf("skill record store is not configured")
	}
	s.agent.configMu.Lock()
	defer s.agent.configMu.Unlock()
	if s.agent.cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	cfg := s.agent.cfg.Clone()
	if cfg.Skills == nil {
		cfg.Skills = map[string]skill.Record{}
	}
	record.Reasons = append([]string(nil), record.Reasons...)
	cfg.Skills[name] = record
	modTime, err := stageAndCommitConfig(cfg, nil)
	if err != nil {
		return nil, err
	}
	s.agent.cfg = cfg
	s.agent.configModTime = modTime
	return cloneAgentSkillRecords(cfg.Skills), nil
}

func cloneAgentSkillRecords(in map[string]skill.Record) map[string]skill.Record {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]skill.Record, len(in))
	for name, record := range in {
		record.Reasons = append([]string(nil), record.Reasons...)
		out[name] = record
	}
	return out
}
