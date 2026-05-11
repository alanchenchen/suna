Select the best model and recommended tools for this sub-task.

Available models (with strengths):
{{.Models}}

Available tools: readfile, listdir, readhttp, exec, writefile, editfile, writehttp

Task: {{.Task}}

Reply JSON only:
{ "model": "provider/model", "tools": ["readfile", "listdir", ...] }

Rules:
- Grant minimal tools needed for the task
- Default: readfile, listdir, readhttp, exec (safe for most tasks)
- Only add writefile/editfile if the task requires file modification
- Only add writehttp if the task requires HTTP POST/PUT
- Never include spawn
