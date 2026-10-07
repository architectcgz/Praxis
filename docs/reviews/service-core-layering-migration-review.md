# Service / Core 分层迁移方案 Review

## 结论

**方向可行，建议修订后实施。确认 7 项问题：高 1 项、中 6 项。**

复用现有 service facade、将模型工具数据契约移入 contracts、使用窄 filesystem Port 都有明确用途，无需新增统一 Domain Facade 或通用转换框架。主要问题是阶段验收互相依赖、错误协议兼容性没有落实，以及验证流程无法证明所声明的迁移结果。

评审对象：[service-core-layering-migration.md](../../.architecture/plans/service-core-layering-migration.md)。基线为 `313b9f6f2af0bd7073a89da8a174f363489498a4` 加当前工作树；包含尚未提交的 core/model 文件拆分。此次只新增评审报告，未修改方案或业务代码。

评审参照本地 `code-review@claude-plugins-official` 的证据核验、历史上下文和误报过滤规则。该插件本地入口是 PR review command，本次适配为本地方案评审，未执行 PR 多 agent 流程或发布 GitHub 评论。

## 问题

### R1【高】M1 的独立验收依赖尚未执行的 M2

- **位置**：[M1 interface](../../.architecture/plans/service-core-layering-migration.md#L180)、[M1 验收](../../.architecture/plans/service-core-layering-migration.md#L234)、[M2 工作项](../../.architecture/plans/service-core-layering-migration.md#L250)。
- **问题描述**：M1 要求整个 `backend/wails` 不再引用 core，并要求 `ModelConfigService` 改收 service request；但是删除 `core/task.MaxInputBytes`、迁移 `PrepareModelConfig` 和 `ValidatedConfig` 构造、移除配置错误类型依赖都排在 M2。这些正是完成 M1 所需的工作。
- **代码证据**：[validation.go:12](../../backend/wails/validation/validation.go#L12) 仍引入 core/task；[model_config.go:14](../../backend/wails/validation/model_config.go#L14) 返回 core `ValidatedConfig`；[bindings_model_config.go:57](../../backend/wails/bindings/bindings_model_config.go#L57) 使用该构造结果并识别 core 校验错误。
- **影响范围／风险**：按阶段执行时，M1 无法达到自己的完成条件。实施者必须临时提前做 M2、留下闲置的 core 构造代码，或临时放宽验收，独立提交与回滚边界随之失去确定性。
- **修正建议**：把配置准备及相关错误映射迁入 M1，M2 只清理其他重复规范化；或明确 M1 只收敛查询 view 和事件，保留配置／validation 的临时例外，到 M2 才执行整个 Wails 包的零依赖门禁。
- **可选方案**：合并 M1、M2 为一个完整边界迁移阶段，再按编译通过的中间状态拆 commit。不要继续同时承诺“依赖清零”和“清理工作下一阶段再做”。

### R2【中】校验下沉会改变现有错误码和配置保存的返回通道

- **位置**：[M2 工作项](../../.architecture/plans/service-core-layering-migration.md#L251)、[M2 验收](../../.architecture/plans/service-core-layering-migration.md#L279)。
- **问题描述**：方案要求删除 Wails 对 content/prompt 的业务校验、统一配置校验错误，同时承诺 wire 和错误分类不变，但没有规定旧错误协议如何映射。
- **代码证据**：
  - [ValidateSendInput](../../backend/wails/validation/validation.go#L93) 的空 content，以及 [ValidateQueueTask](../../backend/wails/validation/validation.go#L121) 的空／过大／非法 UTF-8 prompt，目前返回 Wails `invalid_request`。
  - 下层 [start.SendInput](../../backend/internal/service/runtime/task/start/immediate.go#L40) 和 [queue.EnqueueTask](../../backend/internal/service/runtime/queue/service.go#L79) 返回 `contracts.InvalidValue`；经 [PublicError](../../backend/wails/error/errors.go#L40) 编码后是 `generic.invalid_value`。
  - 未知 Provider 引用由 [ValidateSaveModelConfig](../../backend/wails/validation/validation.go#L167) 返回 `validation.Error`，经 [PrepareModelConfig](../../backend/wails/validation/model_config.go#L23) 原样传出；[SaveModelConfig binding](../../backend/wails/bindings/bindings_model_config.go#L57) 当前将其作为调用错误返回。只有识别为 `modelconfig.ValidationError` 的错误才进入成功返回值中的 `validationError` 字段。
- **影响范围／风险**：直接删除边界校验后，同一请求的 error code 和 message 会变化；将所有配置校验错误统一映射进 `validationError`，还会将部分原先的调用失败变为正常 response。单纯检查 JSON 字段名或 service 是否拒绝输入无法发现这一变化。
- **修正建议**：在 M0 固定“输入场景 → 调用成功／失败 → code → message／validationError”的兼容表，M2 明确 service 错误到现有 Wails 协议的映射。校验仍只执行一次，兼容转换由 Wails 负责。
- **可选方案**：若确实决定统一对外错误协议，将其列为显式行为变更并同步前端与验收要求，不再宣称该部分完全兼容。

### R3【中】方案依赖的 runtime 回归测试基线实际不存在

- **位置**：[兼容策略](../../.architecture/plans/service-core-layering-migration.md#L83)、[M0 工作项](../../.architecture/plans/service-core-layering-migration.md#L120)、[M5 测试要求](../../.architecture/plans/service-core-layering-migration.md#L410)。
- **问题描述**：方案写明“先保留行为测试，再替换类型”，并要求工具调用幂等、授权、unknown 结果和取消结算测试保持通过；但扫描当前 `backend`，`*_test.go` 数量为 **0**。M0 只要求执行测试命令和记录行为清单，没有建立这些基线用例。
- **历史证据**：`2e93aef` 的提交主题为移除仓库测试文件；当前文件扫描确认不存在可沿用的 Go 测试。
- **影响范围／风险**：现有 `go test ./...` 无法证明上述行为已回归。M5 更换生命周期 callback 和执行参数，即使编译成功，也无法验证提交失败时不发终态、重复结束不重复通知、取消控制命令优先级和 unknown 不重放等明确承诺。
- **修正建议**：将 M0 改为建立最小行为基线，或要求每阶段在修改实现之前先补对应特征测试。M1 覆盖主要事件及 nullable／零值 JSON，M2 固定错误协议；M5 至少覆盖结束提交失败／重复结束、已提交取消命令仲裁，以及工具调用身份和 unknown 不重放。
- **可选方案**：在 M5 前从历史测试中挑选仍适用的关键场景恢复；无需恢复全部测试或引入新框架。将“保持通过”替换为明确的测试文件、场景和通过证据。

### R4【中】依赖门禁的退出状态与通过条件相反

- **位置**：[M6 依赖门禁](../../.architecture/plans/service-core-layering-migration.md#L435)。
- **问题描述**：所给 `go list ... | rg '禁止依赖'` 在无匹配时返回 **1**，发现违规时返回 **0**；文档却要求“无输出并以成功状态结束”。后面的裸 `rg` 和 `grep` 也有同样问题。
- **验证证据**：使用两条合成 import 行执行文档中的 `rg` 表达式，合法依赖行得到 `exit=1`、空输出；含 `praxis/internal/core/model` 的行得到 `exit=0`、输出违规行。
- **影响范围／风险**：直接接入以退出状态判定成功的 CI，会让合法迁移失败、违规依赖通过。此外，如果 `go list` 本身失败，不能把它产生的空输出误当成无违规。
- **修正建议**：先单独检查 `go list` 是否成功，再判定 `rg` 的状态：匹配到违规返回失败，无匹配返回成功，工具错误保持失败。验收至少覆盖合法依赖、违规依赖、依赖枚举失败三种情况，并明确执行目录。
- **可选方案**：用一个小型 Go 架构测试完成同样检查；仅为这条门禁无需新增脚本框架。

### R5【中】没有重新生成 Wails bindings，前端构建可能验证旧契约

- **位置**：[M1 验收](../../.architecture/plans/service-core-layering-migration.md#L234)、[M6 全量验收](../../.architecture/plans/service-core-layering-migration.md#L453)。
- **问题描述**：M1 会把 Go binding 返回类型从 timing/model 类型改为 DTO，但验收只执行 `npm run build`。该命令实际是 [tsc && vite build](../../frontend/package.json#L8)，不会生成 Wails 类型声明。
- **代码证据**：[api/bindings.ts](../../frontend/src/api/bindings.ts#L1) 直接依赖 `frontend/wailsjs/go/bindings` 的 generated declarations；当前本地 `AgentBindings.d.ts` 仍返回 `timing.Record` 和 `model.ModelUsageRecord`；生成目录被 [.gitignore](../../.gitignore#L13) 忽略，不会自动随 backend 修改更新。
- **影响范围／风险**：开发者本机可能拿旧声明构建成功，干净检出则没有这些声明；验收结果不能证明新 Go 出口与 TypeScript 消费端一致。
- **修正建议**：在 M0、M1 及最终验收的前端构建前，从 `backend/` 执行 `wails generate module`，再在 `frontend/` 执行 `npm run build`；其他目标平台按项目配置传入所需 build tags。检查生成结果中的 usage/timing 类型均来自 DTO。
- **验证边界**：已确认项目和本机 CLI 都是 Wails **v2.14.0**，并核对该版本 `generateModule` 调用 `bindings.GenerateBindings`。本次只查看版本、help 和实现，未实际生成文件、构建应用或启动开发服务。

### R6【中】M5 漏掉公开配置中的具体 runtime Registry

- **位置**：[目标依赖图](../../.architecture/plans/service-core-layering-migration.md#L12)、[M5 目标](../../.architecture/plans/service-core-layering-migration.md#L386)、[M5 验收](../../.architecture/plans/service-core-layering-migration.md#L410)。
- **问题描述**：M5 只迁移 TurnParams、ToolInvocationMetadata 和 AgentEventObserver，却要求 service/runtime 的公开签名不出现 agentruntime 类型、service 面向中立 runtime Port。
- **代码证据**：[service/runtime.Config](../../backend/internal/service/runtime/service.go#L16) 仍有公开字段 `Registry *agentruntime.Registry`，Service 同样持有该具体对象，并在 [activate](../../backend/internal/service/runtime/service.go#L65) 中调用它。该文件不在 M5 的迁移工作项中，验收正则也只匹配三个被点名的类型，发现不了 Registry。
- **影响范围／风险**：即使完成 M5 的全部工作项且正则通过，目标依赖仍未闭合，service/runtime 继续绑定执行器的具体实现；“公开签名不泄漏 runtime 类型”的完成条件不成立。
- **修正建议**：消费方定义仅含当前 `Activate` 能力的最小接口，由 compose 注入 Registry，保留 activate 中已有的提交后激活及 Abort 补偿逻辑。这是对现有依赖的收窄，不需要通用 runtime Port 包。
- **可选方案**：若本次只收敛业务方法参数，则明确将执行协调服务的 Registry 列为目标图中的例外，并将验收范围同步缩窄；不要让定向正则代表整个目标边界已完成。

### R7【中】规范化 owner 迁移后，仓库 AGENTS.md 仍会要求相反实现

- **位置**：[M2 配置规范化](../../.architecture/plans/service-core-layering-migration.md#L257)、[M6 文档更新](../../.architecture/plans/service-core-layering-migration.md#L427)。
- **问题描述**：方案把模型配置的唯一规范化 owner 改为 service，只安排更新 `.architecture/architecture.md`，没有同步根目录协作规则。
- **代码库约束证据**：[AGENTS.md:43](../../AGENTS.md#L43) 明确要求：“模型配置由 Wails 输入层 `wails/validation.PrepareModelConfig` 统一规范化；service 只接收已准备配置……业务层不执行 Trim 或补默认值。”这与 M2 的目标直接冲突。
- **影响范围／风险**：迁移完成后，后续实现和自动 review 仍会按旧 owner 判定 service 规范化违规，容易重新引入 Wails 构造 core 配置或重复 Trim。
- **修正建议**：把更新 AGENTS.md 列入迁移 owner 的同一阶段提交，改为 service 请求入口负责模型配置规范化，core 构造器和磁盘恢复继续只读校验 canonical 数据。
- **可选方案**：无须保留新旧规则说明；按项目要求直接写最终规则。这里需要补齐迁移工作项，不需要否定本次 owner 调整。

## 建议的最小调整

1. **M0**：确认无测试的现状，建立最小行为基线；保存错误协议和事件 JSON 样本；生成 Wails bindings 后再执行前端构建。
2. **M1／M2**：明确二选一的阶段边界。推荐 M1 先完成 view／event，M2 完成配置和输入 owner，M2 才要求整个 Wails 零 core 依赖；M2 同步更新 AGENTS.md 和错误映射。
3. **M3／M4**：保持窄 filesystem Port 和纯工具数据契约的方向。对 M1 的“只允许标量和 slice”补充可选指针／值类型规则，明确保留 usage、timing 的“未知”与“0”区别，避免实现时补零。
4. **M5**：补上 Registry 依赖的处理决定，先建立关键生命周期和幂等用例，再迁移 adapter。
5. **M6**：修正门禁退出状态，先验证门禁自身的三种结果，再做生成 bindings 和全量验收。

现有代码的终态发布位于首次结束事务成功之后，PreviewFile 使用 root-constrained open 与 bounded read；方案也要求保留配置锁和目录失败回收策略。这些方向合理，本次未将它们列为新发现的迁移缺陷；具体行为仍须通过上述基线验证。

## 检查记录与限制

- 已完整阅读迁移方案、原分层 review、根目录规则和架构文档，并核对 Wails binding／validation、service facade、compose、项目目录、文件预览、模型工具契约及 runtime 生命周期相关实现。
- 已确认当前 Go 版本声明为 1.25.0、Wails 依赖及本机 CLI 为 v2.14.0。
- 已扫描 Go 测试文件、核对相关 git 历史、复现门禁正反退出状态，并验证 Wails generated bindings 的来源和忽略规则。
- 未运行 `go test`、`go vet`、`npm run build` 或 Wails 文件生成；本次是迁移方案评审，未声称业务回归已通过。
- 保留工作树原有方案、原 review 和 model 拆分改动；只新增本报告。
