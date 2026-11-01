# Entry 提交编排与分支数据精简计划

## 背景与目标

会话历史由 Entry 节点、父节点关系和命名分支的 Head 共同表达。模型调用、工具执行、Run 内存接纳、Session 提交和界面通知应有明确的职责与调用顺序。

本次工作统一以下目标：

- 执行服务直接编排 Entry 提交，读代码即可看出何时更新 Run、何时写入 Session 日志。
- Loop 负责上下文构建、模型请求、响应处理与摘要生成能力，不持有隐藏 Session 写入策略的 Entry 集合接口。
- Entry 的顺序由有序列表和 `ParentID` 路径表达，不额外维护 Entry 序号。
- Entry 节点不携带命名分支身份；目标分支由提交参数和日志事件表达。
- 写入失败由当前操作返回，关闭页面和释放会话不因历史写入错误而被阻止。
- 日志只保存实际需要的事件字段，不兼容旧日志格式。
- 会话服务负责事件编解码，Infra 只负责日志完整行的读写。

## 目标状态

### 执行编排

`service/agent/execution` 掌握主执行与子执行的归属，并负责模型结果、工具调用和 Entry 提交之间的流程决策。

`core/loop` 保留上下文构建、模型请求、响应内容转换和摘要生成能力。单个工具由现有工具执行器完成，调用顺序、回执记录和中断收尾由执行服务负责，不通过新增的转发接口重新隐藏持久化。

主执行中，一次带工具调用的模型回合按以下顺序执行：

```text
从 Run 读取当前上下文素材
→ 构建本轮模型请求上下文
→ 请求模型并接收本轮响应
→ 将响应转换为文本、思考、推理状态、工具调用等内容块
→ 根据响应中的工具调用信息及本次执行归属，准备待执行工具调用
→ 执行服务用同一份响应内容及执行归属构造 Assistant Entry
→ 执行服务调用 Session.AppendEntry
   → Session 将 Entry 接纳到内存历史并推进目标分支 Head
   → Session 同步将包含全部响应内容块的 Assistant Entry 追加到 JSONL 日志
→ 执行服务调用 Run.Accept，更新 Run Context 和执行证据
→ 发送相应的 Assistant 后置 Hook 与条目展示通知
→ Assistant Entry 已写入日志并更新 Run 后，执行一个已准备的工具调用
→ 执行服务用该工具的真实回执构造工具回执 Entry
→ 执行服务调用 Session.AppendEntry，由 Session 接纳回执 Entry 并同步追加到 JSONL 日志
→ 执行服务调用 Run.Accept，更新 Run Context 和执行证据
→ 发送相应的后置 Hook 与展示通知
→ 继续后续工具
→ 全部回执处理后，从 Run 读取更新后的上下文素材并构建下一轮请求
```

主执行中，一次不带工具调用、正常完成回答的模型回合按以下顺序执行：

```text
从 Run 读取当前上下文素材
→ 构建本轮模型请求上下文
→ 请求模型并接收本轮响应
→ 将响应转换为文本、思考、推理状态等内容块，确认结束原因为正常完成
→ 执行服务用响应内容及执行归属构造 Assistant Entry
→ 执行服务调用 Session.AppendEntry
   → Session 将 Entry 接纳到内存历史并推进目标分支 Head
   → Session 同步将包含全部响应内容块的 Assistant Entry 追加到 JSONL 日志
→ 执行服务调用 Run.Accept，更新 Run Context 和执行证据
→ 发送相应的 Assistant、最终输出后置 Hook 与条目展示通知
→ 结束本次模型回合与模型工具循环
```

不带工具调用的正常完成回合也会记录 Assistant Entry。该回合结束后进入完整用户轮次的收尾，按既定规则处理轮后摘要和 Run 结算。

上下文素材包括系统提示词和 Run 当前保存的上下文 Entry。每次模型请求前，先读取这些素材，再在请求副本上执行上下文构建 Hook 和 `ContextBuilder.Build`，得到本轮模型请求上下文；请求加工结果不回写 Run Context 或 Session Entry。

Assistant 内容指模型本次响应中的文本、思考、推理状态、工具调用等内容。流式响应在接收过程中汇总，正常结束后转换为内容块，再由执行服务结合角色、Entry ID 和执行归属构造 Assistant Entry。本次响应接收完毕后，仍可能需要执行工具并继续请求模型；取消或模型流错误时，按既定规则提交已收到的部分文本或思考内容。

主执行的 `Session.AppendEntry` 通过 `entry.appended` 事件，将完整 Entry（包含 `Content`）同步写入 JSONL，事件同时携带目标分支。响应中实际收到并经转换保留的内容块均随 Assistant Entry 保存，工具回执另记为 Entry：

| 内容 | 内容块类型 | JSONL 中的记录归属 |
| --- | --- | --- |
| 模型回复文本 | `text` | Assistant Entry 的 `Content` |
| 模型返回的思考内容 | `thinking` | Assistant Entry 的 `Content` |
| 模型返回的推理状态 | `reasoning_state` | Assistant Entry 的 `Content` |
| 工具调用 ID、名称和参数 | `tool_call` | Assistant Entry 的 `Content` |
| 工具执行后的真实回执 | `tool_result` | 工具回执 Entry 的 `Content` |

工具调用的 ID、名称和参数在模型响应解析阶段已经取得，结合本次执行归属即可准备待执行调用。Assistant Entry 记录同一份响应中的调用信息，正常执行直接使用已准备的调用。待执行调用是内容块结合执行归属后的内存表示，其调用信息已经随 Assistant Entry 写入 JSONL，无需额外保存一份待执行调用对象。主执行中，Assistant Entry 写入日志并更新 Run 是开始执行工具的前置条件；工具调用的准备可以在记录 Entry 前完成。启动恢复时，可从历史 Entry 重建调用信息。

接纳 Entry 指将其纳入内存历史或本次执行状态。主执行由执行服务调用 `Session.AppendEntry`，Session 在内存中接纳 Entry、推进目标分支 Head 后同步追加日志，再由执行服务将同一 Entry 引用交给 `Run.Accept`。子执行直接调用 `Run.Accept`，不写入主 Session。

提交失败向当前执行返回错误，并按现有业务语义停止后续流程。Session 已接纳但日志追加失败时，执行服务仍调用 `Run.Accept`，保留与 Session 一致的内存事实，并向上传递保存错误。已发生的内存接纳与工具副作用保留其事实，不通过重试工具修复日志。

Assistant Entry 的提交必须早于工具执行；每条工具回执的提交必须早于后续工具和下一次模型请求。不能将整个回合的 Entry 留到 `executeTurn` 返回后统一保存。

### Run 与 Session 的职责

- 执行服务决定主执行写入 Session、子执行只保存在所属 Run 中。
- `Session` 在会话锁内处理父节点连接、内存接纳、分支 Head 更新和同步日志追加。
- `Run` 管理本次执行的 Context 与执行证据，不负责主会话持久化策略。
- 执行服务将 Session 已接纳的同一 Entry 引用交给 Run，保持内存历史、Context 和执行证据一致。
- Session 已接纳但日志追加失败时，执行服务仍将该 Entry 交给 Run，并向上传递保存错误。
- 前端条目事件由执行服务在明确的接纳边界发布。实时文本增量继续独立发送，不逐 token 写日志。

主执行提交的核心调用关系应能直接读出：

```text
执行服务
→ Session.AppendEntry(ctx, branchID, entry)
→ Run.Accept(acceptedEntry)
→ 按接纳结果处理错误、Hook 和前端通知
```

子执行由执行服务直接调用 `Run.Accept`，通知标记为临时条目，不写入主会话历史。

### 日志编解码职责

`core/session` 定义 Event 和会话状态推进规则，`service/session` 负责 JSONL 事件及业务载荷的编解码，并将解码后的 Event 交给重放流程。

会话服务中的 `eventLog` 实现核心 `EventLog`，将编码后的完整行交给 `LogStore`，读取时将完整行解码为 Event。`infra/storage.SessionLog` 只根据会话元数据定位文件，调用通用 JSONL 能力读取或追加完整行，不接收 Event，也不分派 Entry、Execution、分支、用量或耗时事件。

`entry.appended` 的 JSONL 外层通过 `branch_id` 指定目标分支，`payload` 保存包含全部内容块的完整 Entry。未知事件类型或已删除的旧字段在会话解码边界返回错误。

### 返回值与字段生成

执行服务持有模型响应转换出的内容块及本次执行归属，负责构造和提交 Assistant Entry，并准备待执行工具调用。正常执行所需的调用信息直接来自本轮模型响应，无需经过持久化接口返回后再提取。

`Session.AppendEntry` 可以继续返回已接纳 Entry 的引用及新增标记，供编排层更新 Run、识别重复提交和发布通知。调用方使用 `accepted` 或 `stored` 等能表达接纳结果的名称，避免将它视为一次普通集合追加后的 `message`。

`ParentID` 由 Session 根据目标分支 Head 确定；时间在条目接纳边界确定，已发布条目保持只读。移除字段后重新检查返回值的实际使用，不为减少返回值引入额外查询，也不额外复制一套同形的消息模型。

### 顺序与分支

移除 `ContextEntry.Sequence`，以以下顺序作为依据：

- 主会话历史按 `ParentID` 回溯并反转，得到从根到 Head 的路径。
- Run 的 Entry 列表按接纳顺序维护。
- 最后一个 Assistant 直接从有序执行证据中确定，不比较 Entry 序号。

移除 `ContextEntry.BranchID`，保留以下职责划分：

| 数据 | 职责 |
| --- | --- |
| `ContextEntry.ID` | 标识历史节点 |
| `ContextEntry.ParentID` | 连接父节点，形成历史树 |
| `Branch.ID` | 标识命名分支 |
| `Branch.HeadEntryID` | 指向该命名分支的当前位置 |
| `ExecutionRecord.BranchID` | 标识执行归属 |
| 页面绑定、Task、事件来源中的 `BranchID` | 表达页面选择、调度隔离和通知归属 |
| `entry.appended` 日志事件中的 `BranchID` | 记录本次提交推进的目标命名分支 |

多个分支可以共享同一个 Head 或祖先节点，因此日志重放不能仅凭 `ParentID` 推断应该推进哪个命名分支。Entry 追加事件携带目标 `BranchID`，业务重放据此接纳节点并更新对应 Head。

参考 Pi 的分离方式：模型消息不承担会话树的全部元数据，外层会话编排负责将完成的消息提交到历史。Praxis 保留多个命名分支及其独立 Head，分支身份仍用于页面和执行隔离。

## 任务清单

### 会话写入基础

- [x] 会话写入点直接构造 `Event` 并调用 `EventLog.AppendEvent`。
- [x] 移除 `Session.appendEvent` 包装。
- [x] 移除 `saveError`、`PersistenceError`、`ErrUnsavedChanges` 和关闭时的持久化错误检查。
- [x] 移除前端 `session.unsaved_changes` 错误码及对应文案。
- [x] 移除核心事件、存储结构和构造点中的 `Collection` 字段。

### 精简 Entry 序号

- [x] 删除 `ContextEntry.Sequence` 及 JSON 字段。
- [x] 删除 Session 追加 Entry 时的序号分配和重复比较中的序号补齐。
- [x] 删除 Run 中仅为 Entry 服务的 `sequence` 状态、初始化与递增逻辑。
- [x] 删除子执行初始输入的 Entry 序号赋值。
- [x] 修改 `ExecutionProgress.WithEntries`，按有序列表确定最后一个 Assistant。
- [x] 核对前端消费与生成绑定，清理 Entry 序号依赖。
- [x] 移除历史查询中未使用的 `SequenceBoundary` 及集合包装，直接返回有序 Entry 列表。

模型请求序号、Turn ID 派生、Task 队列顺序及 Provider 协议序号各有独立语义，保留这些序号。

### 调整 Entry 与分支归属

- [x] 删除 `ContextEntry.BranchID` 及 JSON 字段。
- [x] 保留 `Session.AppendEntry(ctx, branchID, entry)` 的显式目标分支参数。
- [x] 调整 Entry 接纳和校验，从提交参数或执行归属取得目标分支。
- [x] 为 `entry.appended` 的核心事件与 JSONL 编解码携带目标 `BranchID`。
- [x] 调整 `Data.AcceptEntry` 和日志重放，显式推进目标分支 Head。
- [x] 调整重复提交与 Execution 输入归属检查，不再读取 Entry 自身的 `BranchID`。
- [x] 核对 `TreeNode`、历史查询与前端树展示，清理依赖节点 `BranchID` 的映射；节点与路径由 ID、父节点和分支 Head 表达。
- [x] 保留 Branch、Execution、Task、页面绑定、审批、调度索引和通知来源的分支身份。

### 将提交职责放回执行服务

- [x] 调整 `executeTurn` 及其调用边界，使执行服务直接掌握模型完成、Assistant 提交、工具执行和回执提交的顺序。
- [x] 移除 `ExecutionDependencies.Entries` 和混合职责的 `EntryOperations`，不以另一套同等职责的注入接口替代。
- [x] 梳理 `loopExecution`；迁移其中 Entry 提交、上下文取得、通知发布和错误分类职责，删除失去用途的包装。
- [x] 主执行由编排层显式提交 Session，再将已接纳 Entry 引用交给 Run；子执行直接接纳到 Run。
- [x] 调整正常 Assistant、流中断后的部分文本、工具真实回执及中断回执的提交路径。
- [x] 将摘要的提交、Run Context 更新和条目通知放到执行服务；摘要生成与模型请求能力按其职责保留。
- [x] 用户输入、Assistant、工具回执和摘要遵循一致的接纳约定，但保留各自实际业务时序，不增加通用提交框架。
- [x] 梳理提交后的 Hook 和前端通知，避免重复通知，并维持只读约定。
- [x] 同步相关源码注释、README 和直接受影响的架构说明，只描述最终职责与行为。

### 会话事件与文件读写边界

- [x] 会话服务负责 Event 与 JSONL 完整行之间的编解码。
- [x] Infra 日志接口只读写完整行，不依赖 Event 或事件类型分派。
- [x] 未知事件和旧字段在会话解码边界返回错误，不保留旧日志兼容分支。

## 关键约束

1. 写入错误通过当前操作和执行错误路径向上传递；页面关闭继续等待运行收尾并释放会话，不增加未保存状态或保存确认流程。
2. 保留内存先接纳、同步追加日志的约定，不增加回滚、自动补写或工具重执行。
3. 重复 Entry 复用已有接纳结果，不重复写入或发送提交通知。
4. 取消和模型流错误时，按现有规则接纳已收到的文本或思考内容；工具回执使用独立的收尾提交上下文。
5. 已完成工具的真实回执保持不变，中断回执只处理尚未取得回执的调用。
6. 摘要在完整用户轮次收尾后处理；摘要失败通过现有错误与日志路径报告，不改变已完成回答。
7. 不为本次调整增加事件总线、通用存储框架、额外防御性校验或无实际职责差异的类型。
8. 不保留旧 JSONL 的兼容字段、迁移或自动修补逻辑；不删除或改写真正的用户数据。

## 风险与回滚

- JSONL 结构调整后，带有已删除字段的历史文件不在读取范围内。验证使用独立临时数据目录；程序回滚时使用与程序版本匹配的数据备份。
- 改变编排位置可能影响 Assistant 提交与工具副作用的先后顺序，必须逐一核对正常、失败、取消及摘要路径。
- 两个命名分支可能指向同一父节点，日志事件必须保留提交目标，确保恢复后只推进正确的分支 Head。
- 删除 Entry 序号后，依赖 Entry 的处理必须遵守路径或接纳顺序，不改用时间戳推断先后。
- 代码回滚应覆盖模型字段、事件编解码、恢复和编排调用的整组改动；涉及生成绑定时一并从源头重新生成。

## 验证与验收

按仓库约定执行静态检查和构建：

```bash
go -C backend build ./...
go -C backend vet ./...
go -C backend list ./...
npm --prefix frontend run check
```

若 Wails 导出的签名或类型变化影响绑定，在 `backend/` 执行 `wails3 task bindings` 重新生成，再执行前端检查；不手工修改生成文件。

检查内容：

- 源码及 Wails 生成绑定无 `ContextEntry.Sequence`、`ContextEntry.BranchID` 和 `ExecutionDependencies.Entries` 的残留使用，其他有独立语义的序号和分支字段保持正确。
- 阅读编排入口即可定位 Assistant、工具回执和摘要的提交位置与失败处理。
- 前端收到已接纳 Assistant 条目后，按执行身份和 Assistant 条目 ID 隐藏同一回合的流式副本。
- JSONL 事件编解码位于会话服务，Infra 日志接口只处理完整行。
- 普通问答、连续工具调用、工具失败、模型流中断、执行取消及摘要路径维持规定的提交顺序。
- 不带工具调用的正常回答也将文本、思考和推理状态等实际收到的内容块随 Assistant Entry 写入 JSONL，记录成功后结束模型循环。
- 带工具调用的 Assistant Entry 在工具执行前写入 JSONL，包含本次响应的文本、思考、推理状态和工具调用；各工具真实回执分别以工具回执 Entry 写入。
- 主执行更新 Session 历史；子执行只更新自身 Run。
- 两个分支共享祖先或同一 Head 后分别追加，重放后恢复各自 Head 和历史路径。
- 主会话日志写入失败会报告当前操作错误，同时允许页面在执行收尾后关闭。
- 不新增、保留或运行自动化测试，不启动开发服务；人工回归使用已有运行环境和独立临时数据目录。

构建与静态检查用于核对接口和类型；桌面交互、工具副作用及重启恢复仍需在独立数据环境完成人工回归。
