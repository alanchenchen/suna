package config

import "github.com/alanchenchen/suna/internal/skill"

func (c *Config) LoadSkillRecords() map[string]skill.Record {
	return cloneSkillRecords(c.Skills)
}

func (c *Config) SaveSkillRecords(trust map[string]skill.Record) error {
	c.Skills = cloneSkillRecords(trust)
	return c.Save(c.ConfigPath())
}
