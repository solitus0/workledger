# Local worklog context plan

Implementation order:

1. Reuse local worklog date-window selectors and timezone rules.
2. Build read-only day snapshots over canonical local worklogs.
3. Add prorated free-slot and collision analysis from effective workday settings, including cross-midnight occupancy slices inside each selected local day.
4. Harden `table` and `json` rendering for planning inspection, baseline `planning.issues[*]` fields, additive issue metadata hints, and external payload generation.
