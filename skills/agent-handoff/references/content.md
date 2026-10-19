# Content draft (schema 1)

The draft is the `content` object only. baton supplies revision, source/session/time, project/task IDs, parent digest and workspace metadata. Use UTF-8 JSON, no duplicate or unknown keys, and no trailing values.

```json
{
  "goal": "Implement stable cursor pagination",
  "acceptance": ["Equal timestamps cannot duplicate items across pages"],
  "non_goals": ["Deployment"],
  "completed": ["Reviewed query ordering"],
  "in_progress": ["Cursor validation"],
  "decisions": [{"decision": "Use ID as tie-breaker", "rationale": "Timestamps can coincide", "evidence": ["Synthetic design review"]}],
  "blockers": [],
  "next_steps": ["Test equal timestamps and invalid cursors"],
  "verification": [{"check": "Unit tests", "result": "not_run", "evidence": "Implementation incomplete"}],
  "unverified": ["Real client handoff"],
  "evidence": [],
  "permission_reminders": ["Deployment requires separate authorization"]
}
```

`goal` must be nonempty; each decision needs its rationale. Verification results are `passed`, `failed`, `not_run` or `partial`. Empty arrays mean no entries were recorded, not that a check passed or a permission was granted. Reference long evidence by an exact scoped path or URL; do not load it all by default. State has an 8 MiB resource limit; this is separate from the soft Markdown character budget.
