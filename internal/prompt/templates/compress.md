Compress the following Suna conversation history into a bounded Session State for a general-purpose agent.

This output is internal working memory, not a user-facing summary. It must let a future agent continue the current session while also recalling earlier completed work or topics if the user asks. Prioritize accurate continuity over transcript detail.

Goals, in order:
1. Preserve the active context so the current task or conversation can continue without interruption.
2. Preserve completed work and earlier topics as a compact ledger, even when they are no longer active.
3. Preserve explicit user requirements, corrections, preferences, accepted decisions, and rejected directions.
4. Convert tool calls/results into concise facts: what was done, changed, discovered, failed, created, or verified.
5. Reduce token usage by dropping raw logs, redundant phrasing, stale speculation, and long file/output contents.

Rules:
- Write in the conversation's primary language.
- Be concise but specific. Prefer bullets.
- Do not invent facts.
- Preserve media reference summaries verbatim (lines like `[image: ...]` with a `source=` value); keep them in the ledger or active context so the agent can re-read the original media later. Do not summarize or drop them.
- Do not include raw tool logs or raw file contents unless an exact short snippet is essential.
- Merge with the previous Session State; do not append duplicate summaries.
- Keep the output bounded. Older completed work may become a one-line ledger item, but should not disappear if it may help recall the session.
- Keep section order exactly as specified. Use "- none" for empty sections.

Use this exact structure:

# Session State

## Active context
- ...

## Completed work / topic ledger
- ...

## User requirements and decisions
- ...

## Tool facts
- ...

## Open threads
- ...

## Recovery note
- ...

{{if .PreviousState}}
Previous Session State to merge and update:

{{.PreviousState}}
{{end}}

New conversation history to fold into the Session State:

{{.Content}}
