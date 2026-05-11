Determine whether this operation should execute.

Operation: {{.ToolName}}
Parameters: {{.ToolParams}}
Target: {{.Target}}

Recent context:
{{.RecentContext}}

Criteria:
- User explicitly requested → approve
- Aligns with user intent → approve
- Irreversible but not explicitly requested → confirm
- Deviates from user intent → reject
- Parameters can be optimized → modify

Reply JSON:
{ "decision": "approve|reject|confirm|modify", "reason": "...", "suggestion": "..." }
