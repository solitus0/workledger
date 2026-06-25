---
name: no-backcompat-coding
description: Enforce clean-break coding: prefer native capabilities, delete custom complexity, treat code as liability, and make compatibility opt-in.
---

# No Backcompat Coding

## Governing Philosophy: Code Is Liability

Every line of code creates continuing costs:

- more behavior to understand
- more states and branches to reason about
- more tests, documentation, and tooling to maintain
- more failure, security, and operational surface
- more constraints on future changes
- more opportunities for obsolete behavior to survive

Existing code is sunk cost, not an asset that must be preserved.

The objective is not the smallest diff. The objective is the smallest clear system that fully implements the current requirements.

A small patch that preserves obsolete branches is often worse than a larger patch that deletes them.

Use this preference order:

1. No code.
2. Deleted code.
3. Native capability from the current stack.
4. Existing simple current code.
5. Small direct project-specific code for the current requirement.
6. A reusable abstraction that demonstrably removes more liability than it adds.
7. Compatibility machinery, only when explicitly required.

Do not confuse fewer lines with lower liability. Dense, indirect, or clever code can be more expensive than readable code. Minimize concepts, contracts, states, branches, dependencies, persisted shapes, and maintenance obligations, not merely line count.

## Core Rule

Default to a clean replacement.

Do not preserve backward compatibility unless the user explicitly requests it in the current task.

Silence is not a compatibility requirement. None of the following count as an explicit request:

- existing legacy code
- old callers
- old tests or fixtures
- deprecated documentation
- persisted obsolete state
- existing compatibility adapters
- historical behavior
- concern that an unknown consumer might still depend on it

Update in-scope callers to the current contract instead of preserving the old contract for them.

## Native Before Custom

Use native language, runtime, framework, library, database, build-system, or platform behavior whenever it fully satisfies the current requirement and removes custom complexity.

Native capabilities take precedence over custom implementations when they:

- satisfy the current contract
- remove custom code, state, branching, configuration, or tests
- follow established framework lifecycle and conventions
- avoid duplicating behavior already maintained by the underlying platform
- do not introduce a larger dependency or operational burden

When native behavior is sufficient:

- delete the custom implementation
- update callers to use the native contract directly
- remove wrappers that merely rename or forward to native APIs
- remove custom tests that duplicate upstream behavior
- retain tests only for project-specific configuration, integration, and invariants
- use native defaults rather than recreating them in application code

Do not preserve custom logic merely because it already exists, appears more flexible, or makes the immediate diff smaller.

Existing custom code is sunk cost. Native maintained behavior usually has lower long-term liability than an application-owned substitute.

## Native Selection Rule

Before adding or retaining custom logic, determine whether the current stack already provides the required behavior.

Use this decision order:

1. Use an existing native capability directly.
2. Configure or compose existing native capabilities.
3. Add a small project-specific integration around native behavior.
4. Add custom logic only when native behavior cannot satisfy a concrete current requirement.

Custom implementation requires a specific justification. “More control,” “future flexibility,” “consistency with old code,” or “we already have it” are not sufficient.

Do not use native behavior blindly. A custom implementation may be justified when the native option:

- cannot satisfy a required invariant
- introduces materially greater complexity
- requires an unwanted dependency or service
- is unstable, deprecated, unsafe, or unsuitable for production
- creates unacceptable performance or operational constraints
- prevents required testing, observability, or error handling

When rejecting a native option, state the concrete requirement it fails.

Do not build a generalized abstraction around a native feature unless the abstraction removes more liability than it creates.

## Decision Order

Apply these decisions in order:

1. **Define the current contract.** Determine the behavior, inputs, outputs, invariants, and data model required now.
2. **Prefer no implementation.** Remove unnecessary behavior instead of rebuilding it.
3. **Prefer native behavior.** Use capabilities already provided by the language, runtime, framework, library, database, build system, or platform.
4. **Configure before coding.** Prefer native configuration or composition over custom implementation.
5. **Replace instead of wrapping.** Use the native or current contract directly rather than retaining obsolete wrappers.
6. **Minimize project-owned logic.** Add only the custom code required to bridge a real gap.
7. **Collapse alternatives.** Keep one implementation, representation, execution path, and source of truth.
8. **Update direct consumers.** Change all in-scope callers to the selected current contract.
9. **Delete superseded surfaces.** Remove obsolete custom logic, adapters, aliases, flags, tests, and documentation.
10. **Verify absence.** Search for removed names, duplicated native behavior, and obsolete concepts.

When several implementations satisfy the requirement, choose the one with fewer enduring concepts, branches, modes, dependencies, persisted shapes, and operational responsibilities.

## Required Behavior

For refactors, rewrites, and replacements:

- Inspect the requested scope before editing.
- Identify the current contract.
- Inspect the current language, framework, libraries, database, and platform for native support.
- Identify obsolete contracts and compatibility surfaces within scope.
- Identify custom code that duplicates sufficient native behavior.
- Prefer native framework and library lifecycles over application-managed equivalents.
- Replace obsolete behavior rather than adapting around it.
- Replace custom implementations with native behavior when the current contract remains satisfied.
- Configure or compose native capabilities before writing custom logic.
- Update every in-scope caller to the current contract.
- Remove superseded code in the same change when practical.
- Remove wrappers, helpers, configuration, state, and tests made unnecessary by native adoption.
- Delete tests, fixtures, mocks, types, schemas, and documentation that exist only for removed behavior.
- Remove unused dependencies introduced solely for obsolete paths.
- Remove comments, examples, and configuration samples that describe removed behavior.
- Rewrite current documentation as though the obsolete design never existed.
- Keep project-specific code limited to business rules and integration requirements not already supplied by the platform.
- Document why custom logic is necessary whenever an applicable native capability was rejected.
- Avoid speculative hooks, extension points, and generalized abstractions for hypothetical future needs.

Do not preserve code merely because deleting it enlarges the patch.

Deletion is implementation work.

## Compatibility Is Explicitly Opt-In

Compatibility support may be added only when the user explicitly requires one or more of the following:

- old clients or callers must continue working
- a public API or published format must remain stable
- existing persisted data must be retained
- a phased or mixed-version rollout is required
- zero-downtime migration is required
- old and new versions must coexist
- rollback must preserve the previous format
- audit, legal, regulatory, or historical retention is required

When compatibility is requested, constrain it precisely:

- identify the consumers or data that require it
- define the supported compatibility window
- isolate compatibility logic from the current implementation
- avoid expanding support beyond the stated requirement
- define a removal condition or sunset when appropriate

Do not turn a narrow compatibility requirement into a permanent second architecture.

## Forbidden Defaults

Do not introduce or preserve these unless explicitly requested:

- backward-compatibility adapters
- compatibility shims
- deprecated aliases
- old parameter parsing
- legacy config-key aliases
- fallback paths for removed behavior
- silent coercion of obsolete inputs
- dual-read or dual-write paths
- multiple persisted representations of the same state
- version negotiation for obsolete clients
- migrations whose sole purpose is preserving obsolete state
- feature flags selecting old versus new behavior
- API response compatibility wrappers
- legacy serialization or deserialization paths
- deprecated types, fields, methods, or endpoints
- compatibility-only error handling
- tests for removed inputs or behavior
- fixtures for obsolete data formats
- documentation for removed parameters, flags, or states
- migration guides that were not requested
- wrappers that retain old names around new implementations
- speculative abstractions added for possible future compatibility
- temporary compatibility code without a required consumer and removal condition
- custom implementations of existing native capabilities
- wrappers that only rename or forward native APIs
- application-managed state already managed by the framework
- duplicated validation, parsing, serialization, routing, retry, cache, scheduling, lifecycle, dependency-management, or state-management logic
- custom defaults that reproduce native defaults
- abstractions created solely to hide direct use of a stable native API
- tests that duplicate upstream framework or library behavior
- compatibility layers around superseded custom implementations after native adoption
- “future flexibility” layers without a current consumer

Do not quarantine obsolete behavior behind a flag. Delete it.

Do not rename legacy code and call the cleanup complete.

## Inputs and External Boundaries

Unsupported old inputs should not be translated into the current format.

Prefer removing the old parsing or dispatch path entirely.

At an unavoidable external boundary, reject unsupported input clearly and early when an explicit error improves the current contract. Do not add elaborate validation, translation, or recovery solely to make obsolete inputs fail more gracefully.

A clear rejection is not compatibility support. Accepting, translating, or preserving the old behavior is.

## Data and Persistence Policy

Data preservation is a requirement only when the user explicitly makes it one.

Without such a requirement:

- obsolete tables and columns may be dropped
- old snapshots and materializations may be discarded
- caches and generated state may be rebuilt
- local, development, test, and disposable data may be reset
- persisted formats may be replaced
- incompatible schemas may be introduced
- rematerialization may replace migration
- obsolete state may be abandoned rather than converted

Do not add dual reads, dual writes, conversion layers, shadow state, or transitional schemas solely to avoid data loss.

Clearly identify destructive consequences in implementation plans and final summaries.

Treat data loss as an acceptable design consequence, not as permission to silently execute an irreversible deletion of production source-of-truth data when the task does not authorize that operation.

## Tests

Test only the current contract.

Tests must cover:

- current behavior
- current invariants
- current failure modes
- production acceptance conditions
- project-specific integration with native behavior
- absence of obsolete paths where practical

Delete or rewrite tests that assert:

- removed parameters
- old input formats
- obsolete config keys
- legacy table or document shapes
- previous API responses
- deprecated aliases
- compatibility fallbacks
- removed failure behavior
- behavior already guaranteed by an upstream language, framework, library, database, or platform

Do not retain legacy tests as historical documentation. Version control already performs that archaeological function.

Do not add tests merely to prove that a removed compatibility path still works.

Do not re-test upstream behavior. Test the project’s contract, configuration, integration, and invariants.

## Documentation

Documentation must describe the system in the present tense and expose only the current contract.

Unless explicitly requested, do not add:

- “previously” sections
- deprecated parameter tables
- old-versus-new comparisons
- compatibility notes
- migration instructions
- removed configuration keys
- examples using obsolete formats
- explanations of deleted internal behavior

Do not make new users learn an obsolete system in order to understand the current one.

Existing immutable audit records or historical changelogs may remain when they are outside scope. Do not add new legacy narrative to current product documentation.

## Instructions for Coding Agents

When directing a coding agent, require it to:

1. Inspect the requested scope and identify the current contract.
2. Inspect the current language, framework, libraries, database, and platform for native support.
3. Compare native support against the exact current contract.
4. Prefer native configuration or composition over custom implementation.
5. List obsolete compatibility surfaces before implementation.
6. Identify custom code that becomes redundant.
7. Choose the design with the lowest enduring code and conceptual liability.
8. Implement the current design directly.
9. Update all in-scope callers.
10. Remove obsolete code, types, flags, state, wrappers, helpers, tests, fixtures, dependencies, and documentation.
11. Justify every remaining custom implementation by naming the requirement native behavior cannot satisfy.
12. Run relevant tests, type checks, linters, and build checks.
13. Search the repository for removed names, duplicated native behavior, and obsolete concepts.
14. Report remaining references and explain any that are intentionally outside scope.
15. State any destructive data or schema consequences.

Use direct wording:

> No backward compatibility is required. Code is liability: minimize the amount of behavior, state, branching, abstraction, and infrastructure that must continue to exist. Prefer native language, framework, library, database, and platform behavior whenever native behavior satisfies the current contract and removes complexity. Implement one current contract. Remove obsolete code rather than preserving adapters, aliases, fallbacks, custom substitutes, or transitional paths. Data loss and rematerialization are acceptable unless retention is explicitly required. Tests and documentation must describe only the current behavior.

## Required Structure for Plans and Coding Prompts

### Goal

Implement the current design as the single supported contract, with no backward compatibility unless explicitly requested.

### Current contract

Define the required current behavior, inputs, outputs, invariants, and persisted shape.

### Native capability review

Identify:

- native language, runtime, framework, library, database, build-system, or platform capabilities that satisfy the current contract
- native configuration or composition that can replace custom code
- custom implementations that duplicate sufficient native behavior
- reasons any applicable native capability was rejected

### Liability reduction

Identify:

- code and concepts to delete
- branches or modes to collapse
- state or data shapes to remove
- dependencies or abstractions to avoid
- compatibility surfaces that must not survive
- custom framework substitutes to replace with native behavior

### Scope

List the files, modules, packages, schemas, tests, and documentation included in the change.

### Required changes

- Implement the current contract directly.
- Prefer native capabilities over custom implementation where they satisfy the current contract.
- Configure or compose native behavior before adding project-owned logic.
- Replace obsolete paths rather than wrapping them.
- Update all in-scope consumers.
- Remove legacy adapters, aliases, fallbacks, flags, parsers, and obsolete state handling.
- Remove custom implementations, wrappers, helpers, state, and tests superseded by native behavior.
- Delete superseded tests, fixtures, docs, types, and dependencies.
- Keep new code limited to behavior required by the current contract.

### Explicit non-goals

- Do not preserve old parameters, config keys, data formats, API shapes, or behavior.
- Do not add compatibility layers, transitional modes, or speculative extension points.
- Do not add migration documentation or legacy tests unless explicitly requested.
- Do not preserve obsolete code to reduce diff size or deployment inconvenience.
- Do not retain custom logic when sufficient native behavior satisfies the current contract.
- Do not wrap stable native APIs solely to avoid direct usage.

### Acceptance checks

- The current contract is implemented and tested.
- Applicable native capabilities were evaluated.
- Native behavior is used wherever it satisfies the current contract with less project-owned complexity.
- No custom implementation duplicates sufficient native behavior.
- No wrapper exists solely to rename or forward a native API.
- Remaining custom logic is tied to a concrete project requirement.
- All in-scope callers use the current contract.
- Only one active implementation and data representation remain.
- No obsolete fallback or compatibility branches remain in scope.
- Tests cover current behavior, project integration, and invariants rather than re-testing upstream behavior.
- Documentation describes current behavior only.
- Removed names and concepts are absent from repository searches, except for approved out-of-scope or immutable historical references.
- Every material addition of code, abstraction, state, or dependency is directly justified by a current requirement.
- Destructive data or schema consequences are explicitly reported.
- The resulting system has fewer application-owned concepts and maintenance obligations.

## Final Review Questions

Before completing the work, verify:

- Could any new code be deleted or replaced with an existing current primitive?
- Does the current stack already provide this behavior?
- Can native configuration replace this code?
- Is this wrapper doing anything beyond forwarding or renaming?
- Are we maintaining state the framework already owns?
- Are these tests verifying our contract or merely re-testing a library?
- What exact current requirement prevents deletion of this custom implementation?
- Would using the native contract directly remove an abstraction, branch, dependency, or state transition?
- Is any branch present only for an old input, state, caller, or behavior?
- Does more than one contract or representation remain?
- Was an abstraction added for a hypothetical future need?
- Does any test or documentation preserve obsolete behavior?
- Is the patch optimizing for a small diff instead of a smaller system?
- Does every remaining line earn its ongoing maintenance cost?

If the answer reveals unnecessary liability, remove it before completion.

## Safety Boundary

Do not apply the clean-break default against an explicit requirement for:

- backward compatibility
- phased rollout
- public API stability
- mixed-version operation
- data retention
- audit preservation
- legal or regulatory history
- reversible deployment
- production source-of-truth preservation

Follow the stated constraint narrowly. Preserve only what the requirement actually protects, and do not use it as justification for unrelated legacy machinery.
