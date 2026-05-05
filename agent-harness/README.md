# Agent Harness

This harness defines a three-agent workflow for AutoCache changes:

1. `planner` turns a request and repository facts into a decision-complete implementation plan.
2. `generator` implements exactly that plan and records verification evidence.
3. `reviewer` reviews the generated change for correctness, scope drift, missing tests, and maintainability.

The harness is intentionally document-first. It does not require a runtime orchestrator or external API. A human, Codex session, or CI wrapper can run each role by copying the matching template and the handoff artifact.

## Workflow

1. Create a handoff artifact from `templates/handoff.md`.
2. Run the `planner` role with the user request and current repository context.
3. Put the planner output in the handoff artifact.
4. Run the `generator` role with the planner output.
5. Put the generator summary and verification evidence in the handoff artifact.
6. Run the `reviewer` role with the planner output, generator output, and current diff.
7. If reviewer reports blocking findings, return to `generator`.
8. If reviewer reports no blocking findings, the change can move to normal branch completion.

## Role Boundaries

- `planner` may inspect the repository but must not change files.
- `generator` may change files but must not redefine scope or silently change the plan.
- `reviewer` must not fix code while reviewing; it reports findings and sends work back to `generator`.
- Any role may stop and request clarification when the current handoff is insufficient.

## Required Artifacts

- Planner output: goal, scope, implementation steps, files or subsystems, test plan, risks.
- Generator output: changed files, behavior summary, verification commands and results, deviations from plan.
- Reviewer output: blocking findings, non-blocking findings, test gaps, final disposition.

## Completion Standard

A cycle is complete only when:

- The generator has implemented the planner's accepted scope.
- Verification evidence is attached to the handoff.
- The reviewer has no blocking findings.
- Any remaining risk is explicitly listed.
