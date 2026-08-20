<!-- BEGIN HARNESS ENGINEERING: root-navigation -->
## Harness Engineering

当前采用通用的本地 harness 形态，并保留 `deusyu/harness-engineering` 的核心原则作为重要参考。

| 路径 | 内容 | 说明 |
|------|------|------|
| `.arccgz-harness/state/` | 当前任务状态 | 只保存短期执行证据和当前 reuse 决策 |
| `.arccgz-harness/state/reuse-index/` | 本地私有索引 | 用户自用的长期复用线索，默认 gitignore，`index.yaml` + 镜像 `README.md` |
| `.arccgz-harness/harness/policies/` | 项目策略 | 项目级复用和约束配置 |
| `.arccgz-harness/harness/templates/` | 模板 | 当前项目重复使用的决策或记录模板 |
| `.arccgz-harness/harness/prompts/` | Prompt 入口 | 仓库内稳定入口、局部补充，以及仍然项目专属的 prompt |
| `.arccgz-harness/feedback/` | 反馈记录 | 踩坑、修正和可复用流程经验 |
| `.arccgz-harness/docs/documentation-rules.md` | 文档规范 | 改文档前置读取与新增路径登记 |
| `.arccgz-harness/docs/README.md` | 文档索引 | 当前事实源地图和文档阅读顺序 |
| `.arccgz-harness/docs/architecture/` | 架构事实 | 当前系统设计、边界和长期技术约束 |

项目根保持 `CLAUDE.md -> AGENTS.md`，让 Claude / Codex 使用同一份入口规则。

共享 workflow 不默认安装；需要时显式运行 `bash ~/.agents/harness/init-project.sh <repo-root> --workflow code-workflow`。

开发过程中，如果某个模块第一次形成稳定复用模式，主动补 `.arccgz-harness/state/reuse-index/<source-path>/README.md`；如果模块内部也已经分出稳定层次，再继续补该子路径下的镜像 `README.md`。这是本地提醒，不作为 pre-commit 阻塞项。

如果用户明确要求严格参考 `deusyu/harness-engineering` 的目录形态，再使用 strict reference 模式。
<!-- END HARNESS ENGINEERING: root-navigation -->

<!-- BEGIN HARNESS ENGINEERING: quick-routing -->
## Quick Routing

项目刚起步时这是占位薄壳：通用行可直接用，项目特定行用 `<!-- FILL -->` 占位，起步后替换为真实文件与 skill。压缩后这张表仍是 Agent 查"该读哪些文件 / 用哪个 skill"的线索。

| Task type | Required reads | Workflow / Skill |
|-----------|---------------|------------------|
| Backend feature | `backend/app/`, `backend/internal/`, `.arccgz-harness/docs/architecture/` | `backend-engineer` |
| Frontend feature | `frontend/`, `.arccgz-harness/docs/plan/` | `frontend-engineer` |
| Review | changed files plus the applicable plan and architecture facts | `reviewer` |
| Bug fix | affected backend/frontend path plus the applicable verification command | `systematic-debugging` |
| Add/Edit test | local verification only; test source must not enter the repository | `test-engineer` |
| Architecture change | `.arccgz-harness/docs/architecture/` | `brainstorming` then `writing-plans` |
| Documentation update | `.arccgz-harness/docs/documentation-rules.md`, `.arccgz-harness/docs/README.md` | Direct edit |
| Commit changes | — | `committing-changes` |
| New non-trivial task | Read this AGENTS.md and the relevant project docs | `writing-plans` |
| Other | Read this AGENTS.md fully, then ask user | Direct routing |

## Auto-Triggers

- New task in same session → re-read this AGENTS.md + the relevant skill SKILL.md（"我已经读过了"不算，上下文会压缩）。
- Context compact / clear → SessionStart hook 重新注入 skill bootstrap（若已配置）。
- Before any `git commit` → 先走 `committing-changes` skill 组织提交信息（默认禁止 Co-Authored-By）。
- Task complete (non-trivial) → 跑验证门禁，再做 AAR，若有新模式更新 `.arccgz-harness/feedback/`。
- 起步后把 `<!-- FILL -->` 行替换为本项目真实的必读文件与 skill，替换完运行 `bash .arccgz-harness/scripts/tests/test-trigger-rate.sh` 检查 description 触发率。
<!-- END HARNESS ENGINEERING: quick-routing -->

<!-- BEGIN HARNESS ENGINEERING: todo-reminder -->
## Todo Reminder

开始新任务前，先读取 `.arccgz-harness/docs/todo/` 里的未完成事项；如果命中当前主题，先把它纳入任务范围。已完成但还没归档的 todo 也应顺手处理。
<!-- END HARNESS ENGINEERING: todo-reminder -->

<!-- BEGIN HARNESS ENGINEERING: test-workflow -->
## Test Workflow

- Do not add or commit test source; use local or temporary verification only.
- When running existing or temporary tests, use the smallest relevant command that covers the touched surface.
- Follow any project-specific test, build, or lint checks documented by the repository; the harness does not install a generic follow-up check by default.
- After completing any task that changes backend Go source, run the configured `lll` check:
  `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0 run --config .golangci.yml ./...`.
- The `lll` check must finish with zero issues. When it reports long lines, reorganize the source and rerun the check until it is clean; do not bypass the rule by suppressing findings or increasing the configured line limit.
<!-- END HARNESS ENGINEERING: test-workflow -->

## Code Comment Convention

- Comments in source code and scripts must be written in English.
- Comments should explain business rules, invariants, lifecycle boundaries, side effects, or non-obvious trade-offs; do not narrate straightforward syntax.
- This convention applies to new and modified code in this repository, including Go, TypeScript, CSS, and shell files.

## Backend Layout

- All backend source and backend project files must live under `backend/`.
- Keep Go entrypoints, `go.mod`, `go.sum`, `wails.json`, `app/`, and `internal/` inside `backend/`; do not scatter backend code at the repository root.
- `frontend/` is the only application source directory outside `backend/`. Root-level scripts are repository tooling, not backend source.

## Test Artifact Policy

- Tests may be run locally for verification, but do not add or commit test source files or test fixtures to this repository.
- Do not commit generated test output, coverage files, or local test artifacts.
