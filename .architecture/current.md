# 代码实现梳理与待办

## 一、背景与目标

业务语义和架构约束以 [architecture.md](architecture.md) 为准。本文记录第一轮静态代码梳理确认的问题、目标职责和后续处理顺序，不表示对应改造已经完成。

本轮重点追踪任务提交、分支调度、Run 执行、历史接纳、模型选择及前端分支入口。主要问题是核心概念的实际归属与共识不一致，不是目录或文件数量本身。应先明确所有权和调用关系，再确定必要的类型、接口与目录调整。

目标是：

- 分支独立拥有任务队列、模型选择和执行占用，页面选择不参与执行调度。
- Turn 表示完整 ReAct 迭代，ModelRequest 表示实际模型调用，两者不混用生命周期。
- 已完成 Turn 具有明确的历史边界，分叉使用该边界而不是推测消息位置。
- 历史接纳有唯一责任边界，内部消费已经接纳的事实，不重复审判。
- 保留已有的直接编排、只读历史共享和按需读取能力，不为重构额外增加管理层。

## 二、已确认的问题与目标职责

### 2.1 分支执行状态与会话唯一写入绑定混在一起

代码位置：

- `backend/internal/service/coordinator/coordinator.go`：`bindings` 按 `SessionID` 保存唯一 `sessionBinding`，绑定同时持有分支、任务队列和关闭状态。
- `backend/internal/service/coordinator/task.go`：任务准入要求匹配该会话绑定的分支。
- `backend/internal/service/coordinator/queue.go`：调度从会话绑定取得队列与目标分支。
- `backend/internal/service/coordinator/session.go`：`navigationBinding` 在存在活动 Run 或排队任务时拒绝导航；`releaseBinding` 删除绑定并清空其队列。

当前混乱点：查看哪个分支与哪个分支拥有执行资格被同一份绑定状态连接。活动 Run 的键虽然包含 `BranchID`，但上游队列和准入仍然依赖会话唯一绑定，尚未形成分支独立调度。

目标职责：

- 队列与主代理执行占用按分支持有，同一分支串行，不同分支可并行。
- 切换查看对象不参与执行资格判断，不释放其他分支的执行状态，不清空其队列。
- 关闭整个会话与切换分支是不同操作，不能共用释放语义。
- 控制命令明确定位目标分支和 Run。
- `BindingID` 是否保留取决于梳理后的实际职责，不能只是改名后继续承载会话唯一执行资格。

前端关联：

- `frontend/src/features/agent/agentStore.ts` 的 `receive` 忽略非当前分支事件。
- 应区分当前分支的正文展示与其他分支的轻量运行状态；后台状态不能只由当前页面承载。
- 这不要求前端缓存全部分支正文，也不要求为隐藏分支持续构建完整对话视图。

### 2.2 模型选择仍属于 Session，而不是 Branch

代码位置：

- `backend/internal/service/session/opened_session.go`：已打开会话只有一个 `modelSelection`。
- `backend/internal/service/session/model_selection.go`：模型选择按 `SessionID` 更新和读取。
- `backend/internal/service/session/configuration.go`：`RunConfiguration` 只接受 `SessionID`；配置重载只处理一份模型选择。
- `backend/wails/bindings/bindings_session_model.go`：模型选择入口没有目标分支。

当前实现已经在 Run 准备时固定模型参数，但参数固定之前的选择来源仍是整个会话。

目标归属：

| 内容 | 所有者 |
| --- | --- |
| 模型目录、Provider 凭据、工具与审批配置 | Session 的只读配置 |
| 当前模型与推理等级 | Branch |
| 本次执行实际使用的参数 | Run |

需要一起梳理模型选择的存放位置、入口参数、配置重载和队列启动时的读取位置，不是只给 `SelectModel` 增加一个参数。不同分支共享会话配置，不复制整份目录和凭据。

### 2.3 Turn 与 ModelRequest 的生命周期混用

代码位置：

- `backend/internal/core/agent/run/turn.go`：`TurnRecord` 持有单个 `RequestID`，按实际请求推进序号。
- `backend/internal/service/agent/run/progress.go`：`BeginRequestTurn` 和请求记录一起推进当前 Turn。
- `backend/internal/core/toolexecution/review.go`：风险审核调用 `BeginRequestTurn`。
- `backend/internal/core/loop/request.go`：摘要主动结束并开启 Turn。
- `backend/internal/core/loop/turn.go`：Loop 发布 `TurnCompleted`，但发布位置与持久化结束 Turn 的位置没有统一。

当前混乱点：普通模型提出工具调用后，工具审核会推进另一个 Turn；摘要也占用 Turn 序号。请求身份和迭代身份没有真正分开。

目标职责：

- Turn 由 ReAct 迭代流程开始和结束，包含普通请求、必要审核、工具执行以及输出和回执接纳。
- 普通请求和它触发的风险审核关联同一个 Turn。
- 两轮之间的摘要属于 Run，不强行关联某个 Turn。
- 每个 ModelRequest 独立记录状态与用量，请求记录不负责推进 Turn。
- Turn 完成必须由直接调用确定并记录；通知只描述已确定事实。
- 不为 Turn 和 ModelRequest 分别建立重复的执行管理机制。

### 2.4 分叉依赖 Run 结束节点，缺少 Turn 完成边界

代码位置：

- `backend/internal/core/session/branches.go`：`PrepareBranch` 要求分叉节点满足 `node.Run`，历史树的 `Safe` 同样取自 `node.Run`。
- `backend/internal/core/session/entry/node.go`：`node.Run` 来自 Entry 是否为 Run 结果节点。
- `frontend/src/features/session/useBranchNavigation.ts`：分叉入口使用历史树的安全标记，并限制为已结束执行的末尾。

当前实现只能从 Run 结束节点分叉，不能从某个 Run 内部已完成的 Turn 分叉。

目标职责：

- 执行流程明确记录哪个 Turn 已完成，以及完成时历史截止到哪个 Entry。
- 分叉只接收已完成 Turn 的历史边界，不能从模型增量或未完成工具交互切开。
- 原分支可以继续执行；新分支只继承截至所选边界的历史，不包含原分支后续 Entry。
- 新分支后续任务形成新的 Run，不恢复旧 Run，也不重放历史工具。
- 共享祖先历史，不复制既有正文，不恢复或隔离项目文件。

此项依赖 Turn 语义先落实，不能只放宽 `PrepareBranch` 校验。边界既不能根据最后一条回答推测，也不能依赖前端是否收到过完成通知。

### 2.5 Entry 接纳后仍存在再次拒绝的内部路径

代码位置：

- `backend/internal/core/session/context_entries.go`：Session 负责 Entry 接纳和重复提交判断。
- `backend/internal/core/agent/run/run.go`：`Run.Accept` 接收已接纳 Entry，但仍可能因身份或内容检查拒绝。
- `backend/internal/service/agent/run/entries.go`：先提交 Session，再调用 `Run.Accept`，因此需要处理前者已接纳而后者拒绝的分支。

目标是区分两类职责：

| 职责 | 含义 |
| --- | --- |
| 接纳事实 | 决定是否新增，处理必要的重复提交及归属判断 |
| 消费已接纳事实 | 持有只读引用、更新本次执行统计、发布通知 |

消费方不重新审判已经接纳的内部事实。现有共享只读 Entry 的方向应保留。

不能直接批量删除检查。应先明确正常提交、重复提交和恢复路径各自的入口与负责人，再去除没有独立职责的判断。

## 三、应保留的实现基础

以下能力已有对应实现，不应为了统一形式而重做：

- 调度、Loop、工具执行、历史提交和 Run 收尾通过直接调用连接，关键流程不依赖通知推进。
- Run 运行实例与历史记录已有区分，资源释放不等于删除历史。
- 主代理与子代理已有独立历史路径，子执行通过结果交接，不直接混用父上下文。
- 模型上下文读取与界面分页已有区分，历史正文按需读取。
- 停止命令定位具体分支和 Run，不以命令到达时的当前对象替换目标。
- 重启恢复路径做必要收尾，不自动重放工具或继续旧任务。

运行状态、历史投影与存储索引可能分别承担必要职责，不能因为保存了相关字段就一概认定为重复状态。应按所有权、生命周期和用途逐项判断。

## 四、处理顺序与第一阶段范围

按以下顺序逐项落实到字段、函数和调用路径，不先设计新的 Manager、Service 或通用接口：

1. 分支状态归属：任务队列、模型选择、活动 Run 和页面选择。
2. Turn 完成事实：迭代边界、模型请求关联和历史分叉。
3. 接纳与收尾职责：去除内部重复判断，明确失败后的可见状态。

第一阶段先理清以下执行链：

```text
提交到指定分支
  → 进入该分支队列
  → 占用该分支执行槽
  → 读取该分支模型选择
  → 创建固定配置的 Run
  → 执行并收尾
  → 释放执行槽、调度该分支下一项
```

页面当前查看哪个分支不参与这条执行链。结构调整只服务于这些职责，不机械地为每个概念创建类型或管理器，也不顺带扩大到 Provider、工具协议等无关实现。

## 五、风险与回退边界

- 分支状态调整涉及 Coordinator、会话配置、Wails 入口和前端状态定位，不能只放开导航限制而保留会话唯一队列。
- Turn 与分叉涉及历史记录和索引语义，具体存储调整及已有数据处理方式需要在该阶段确认，不能假设修改校验即可完成。
- 正常重复提交处理与不必要的内部防御检查必须区分，不能以清理为由破坏已接纳结果的确认路径。
- 停止执行不撤销工具已经产生的外部副作用；任何收尾改造都必须保留真实结果。
- 分阶段保持改动范围清晰。代码回退不能被视为历史数据或项目文件回滚，涉及持久化格式时需单独明确兼容与回退条件。
- 本文是静态梳理记录，不包含已经完成改造或运行验证的结论。

## 六、验证方式

实现后按 [architecture.md](architecture.md) 中的场景核对，重点包括：

1. 分支 A、B 同时执行，同一分支内任务保持 FIFO 串行。
2. 查看 B 时，A 的执行、待办与控制目标不变。
3. A、B 分别选择模型，已开始的 Run 参数不随之后的选择改变。
4. 普通请求与审核处于同一 Turn，两轮之间的摘要不占用新 Turn；用量按实际请求累计。
5. 从某个 Run 内已完成 Turn 分叉，只继承截止该边界的历史，未完成交互不能分叉。
6. 没有事件观察者时，历史接纳、Turn 完成、Run 收尾和队列调度仍然正确。
7. Run 资源释放后历史可查询，重新打开应用不重复执行工具。

验证遵守仓库约定：

- 后端使用 `go build ./...`、`go vet ./...`、`go list ./...`。
- Wails 签名变化后在 `backend/` 执行 `go run -tags bindings .` 生成绑定，不手工修改生成文件。
- 前端使用 `npm run check`。
- 不新增、保留或运行自动化测试；数据验证使用独立临时目录，不使用真实用户数据。
- 不自动启动开发服务；需要启动才能人工验证时先取得授权。
