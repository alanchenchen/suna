Extract all facts worth remembering from the following interaction. Include:
- Explicitly stated user preferences/constraints
- Implied user need patterns
- Operations completed by the agent and their results
- Decisions and their reasons
- Errors and lessons learned

Reply in JSON array format:
[{"type": "preference|action|decision|error|fact", "key": "...", "value": "...", "source": "user_stated|agent_confirmed|observed|learned"}]

Interaction:
User: {{.UserInput}}
Assistant: {{.AgentOutput}}
