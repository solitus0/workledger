---
name: feature-plan-scope-trimmer
description: review feature implementation plans, product requirement drafts, technical proposals, tickets, epics, or roadmap slices to identify low-value requirements that disproportionately increase complexity, risk, dependencies, maintenance cost, delivery time, or scope. use when asked to critique a plan, reduce scope, recommend removals, separate must-haves from nice-to-haves, protect an mvp, or flag requirements whose benefit does not justify implementation burden.
---

# Feature Plan Scope Trimmer

## Purpose

Review implementation plans through a value-vs-complexity lens. Recommend removing, deferring, simplifying, or validating requirements when they add disproportionate scope for limited user, business, or technical value.

## Review workflow

1. Identify the feature goal, target users, success metric, and intended release boundary from the plan. If missing, infer cautiously and state assumptions.
2. Break the plan into reviewable requirement items. Treat acceptance criteria, edge cases, integrations, configuration options, admin controls, analytics, migrations, permissions, and non-functional requirements as separate items when they create distinct work.
3. Score each meaningful item using the rubric below.
4. Recommend action for each risky item: remove, defer, simplify, validate first, keep, or keep but constrain.
5. Explain tradeoffs in product language and implementation language. Avoid only saying “too complex”; name the specific complexity source.
6. Preserve the core user outcome. Do not recommend removing requirements that are necessary for safety, legal compliance, data integrity, security, accessibility, observability needed for launch decisions, or the stated success metric.

## Value-vs-complexity rubric

Use these dimensions. Prefer concise qualitative ratings unless the user asks for numeric scoring.

### Value signals

- Directly supports the primary user journey or success metric.
- Unblocks launch, adoption, revenue, retention, compliance, or support cost reduction.
- Addresses a frequent, painful, or high-risk case rather than a rare preference.
- Has evidence from users, support, sales, analytics, incidents, or stakeholder commitments.
- Reduces future complexity more than it creates.

### Low-value signals

- Exists mainly for completeness, polish, theoretical flexibility, or rare edge cases.
- Optimizes for users or workflows not in the release target.
- Adds configuration before there is variation worth supporting.
- Duplicates capabilities available elsewhere.
- Solves a problem that can be handled manually during the first release.
- Depends on unvalidated assumptions or speculative future roadmap needs.

### Complexity multipliers

- Introduces new states, permissions, branching logic, data models, synchronization, migrations, background jobs, caching, or distributed failure modes.
- Requires new external integrations, vendor behavior, cross-team coordination, or data backfills.
- Expands test matrix across roles, locales, devices, plans, tenants, or legacy paths.
- Creates long-term maintenance obligations, support burden, admin UI, documentation, or monitoring.
- Makes rollback, migration, auditability, security review, or debugging materially harder.
- Couples the feature to unrelated systems or blocks independent delivery.

## Recommendation rules

Recommend **remove** when value is low or speculative and complexity/risk is high.

Recommend **defer** when the item may be valuable later but is not needed to prove the release hypothesis.

Recommend **simplify** when the user outcome is valid but the proposed implementation is overbuilt.

Recommend **validate first** when complexity is high and the plan lacks evidence that users need the requirement.

Recommend **keep but constrain** when the item is valuable but should have a narrower launch boundary.

Recommend **keep** when the requirement is required for launch correctness, safety, compliance, security, data integrity, or the primary user outcome.

## Output format

Use this structure by default. Adapt only when the user asks for a different format.

```markdown
# Scope review

## Executive recommendation
[One short paragraph summarizing the biggest scope cuts and why.]

## Recommended removals or deferrals
| Item | Recommendation | Why it expands scope | Value concern | Safer alternative |
|---|---|---|---|---|
| [requirement] | remove/defer/simplify/validate first/keep but constrain | [specific complexity source] | [why value is low, unproven, or non-core] | [smaller path] |

## Keep / protect
- [Requirement that should stay because it is core, safety-critical, compliance-related, or directly tied to the success metric.]

## Suggested lean release boundary
- [Concrete MVP scope after removals.]
- [Explicit non-goals for this release.]

## Open validation questions
- [Question whose answer could change a remove/defer recommendation.]
```

## Style guidelines

Be candid but constructive. Frame cuts as risk reduction, faster learning, and sharper release focus rather than as criticism of the author.

Use decisive language when evidence is clear: “remove this from v1” or “defer until usage shows demand.” Use caveats only when the plan lacks context.

When reviewing engineering plans, call out hidden cost categories explicitly: testing, migrations, permissions, state management, observability, rollout, rollback, and support.

When reviewing product plans, tie recommendations to target persona, job-to-be-done, release hypothesis, and measurable outcome.

## Examples

### Example: configurable notification rules

Input item: “Allow each workspace admin to configure unlimited custom notification rules by role, event type, and delivery channel.”

Recommendation: simplify or defer. This adds rule modeling, permissions, validation, UI, test matrix growth, and support burden before there is evidence that custom rules are needed. Ship one or two default notification paths first; collect usage and support requests before adding a rule builder.

### Example: manual export fallback

Input item: “Add CSV export for failed imports so support can debug customer issues during beta.”

Recommendation: keep but constrain. This supports launch learning and supportability, but it should be limited to internal users during beta rather than a polished customer-facing export system.
