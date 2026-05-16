# Way of Working

Every task follows this strict workflow. Do not skip steps.

## 1. Planning

- Always use the **brainstorming** superskill first for any creative/feature work
- Then use the **writing-plans** superskill to create a detailed implementation plan
- Get user approval on the plan before writing any code

## 2. Development (Strict TDD)

- Use the **test-driven-development** superskill for all implementation
- Write tests FIRST, then implementation
- Each logical task gets its own atomic commit (do not bundle unrelated changes)
- Commit message should clearly describe what the task accomplishes

## 3. Formatting & Linting (after every change)

- Run formatting and linting checks after every code change, not just at the end
- Fix any issues immediately before moving on to the next task
- Commands by project type:
  - **Go projects**: `go fmt ./...`
  - **pdp-cloudflare** (monorepo): `pnpm format:check` from monorepo root (`/Users/kovaron/projects/pdp-cloudflare`), fix with `pnpm format`
  - **Other Node.js/TypeScript projects**: `pnpm run lint` or `pnpm run format` (check project scripts)
- At the end, create ONE separate formatting/linting commit for any remaining changes:
  - Commit message: `chore: format and lint`

## 4. Test Verification

- After all commits, run the full test suite for every affected project
- Report whether test coverage has **increased, stayed the same, or decreased**
- If coverage decreased: ask user to approve or request additional tests
- If relevant, suggest items to exclude from measured coverage (e.g., generated code, test utilities)

## 5. Multi-Round Automated Review & PR Opening

Use the **review-and-open-pr** skill (once available) or follow this manual sequence:

### Review Loop (up to 5 rounds)

Each round consists of:

1. **CodeRabbit CLI review** — run `coderabbit` on the branch diff
2. **Multi-agent review** — use **agent-team-review** superskill (parallel agents for correctness, conventions, contracts, security)
3. **Deduplicate findings** — merge results from CodeRabbit and agents, remove duplicates (match on file + line range + category)
4. **Fix findings** — auto-fix critical and important findings using **receiving-code-review** superskill; log minor findings for PR description
5. **Simplify pass** — run **simplify** skill on the full PR diff (dead code, unnecessary abstractions, clarity improvements)
6. **Re-review** — loop back to step 1

The loop exits when:
- No critical or important findings in a round, OR
- 5 rounds reached (hard cap), OR
- No new findings compared to previous round

Fixing review comments and simplification may produce additional commits per round.
