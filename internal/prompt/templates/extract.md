Extract all facts worth remembering from the following interaction. Include:
- Explicitly stated user preferences/constraints
- Implied user need patterns
- Operations completed by the agent and their results
- Decisions and their reasons
- Errors and lessons learned
- Named entities mentioned (tools, frameworks, libraries, projects, people, files)

Reply in JSON array format:
[{"type": "preference|action|decision|error|fact", "key": "...", "value": "...", "source": "user_stated|agent_confirmed|observed|learned", "entities": ["entity1", "entity2"]}]

If no entities are mentioned, use an empty array for "entities".

Interaction:
User: {{.UserInput}}
Assistant: {{.AgentOutput}}
