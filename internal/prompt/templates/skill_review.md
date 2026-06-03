Review this Suna Skill for safety, clarity, and whether it matches its stated purpose.

Reply in the same language as the user's recent request when it is clear; otherwise use concise English. Include:
1. Summary
2. Risks or issues
3. Recommendation

Do not invent files not shown.

User recent request:
{{.UserRequest}}

Skill: {{.Name}}
Description: {{.Description}}

{{if .Reasons}}
Static check reasons:
{{range .Reasons}}- {{.}}
{{end}}
{{else}}
Static check reasons: none
{{end}}

Files:
{{range .Files}}
--- {{.Path}}{{if .Truncated}} (truncated){{end}} ---
{{.Content}}
{{end}}
