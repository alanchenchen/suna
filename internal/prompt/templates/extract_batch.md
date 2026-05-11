Extract from these interactions:
1. Memorable fact fragments (episodes)
2. Structured user preferences/constraints/habits (facts)
3. Key entity names

{{range .Interactions}}
--- Interaction {{.Index}} ---
User: {{.UserInput}}
Assistant: {{.AgentOutput}}

{{end}}
Output JSON:
{
  "episodes": [{"content": "...", "type": "preference|action|fact|decision", "entities": ["..."]}],
  "facts": [{"key": "...", "value": "...", "type": "preference|habit|constraint|fact", "source": "user_stated|observed"}]
}
