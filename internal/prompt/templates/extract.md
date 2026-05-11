Extract all facts worth remembering from the following interaction. Include:
- Explicitly stated user preferences/constraints (language, coding style, tool preferences, workflow patterns)
- Implied user need patterns (what the user frequently asks for, how they like things done)
- Operations completed by the agent and their outcomes (success/failure, approach taken)
- Decisions and their reasoning (why a particular approach was chosen over alternatives)
- Errors encountered and lessons learned (what went wrong, how it was fixed)
- Named entities mentioned: tools, frameworks, libraries, projects, people, files, URLs, APIs, databases, services

Reply in JSON array format:
[{"type": "preference|action|decision|error|fact", "key": "...", "value": "...", "source": "user_stated|agent_confirmed|observed|learned", "entities": ["entity1", "entity2"]}]

Rules:
- "entities" must list all named things mentioned in the interaction (tools like "Go", "React"; projects like "my-app"; files like "main.go"; APIs like "/api/users")
- "key" should be a short categorization label
- "value" should be the full detail
- Only extract facts that would be useful in future sessions
- Do NOT extract trivial or obvious information
- Preserve the original language of the content (do not translate)

Interaction:
User: {{.UserInput}}
Assistant: {{.AgentOutput}}
