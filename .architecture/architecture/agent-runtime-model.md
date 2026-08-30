# AgentRuntime 与 AgentExecution 执行模型

> 本文定义进程内 AgentRuntime actor、execution activation、model/tool loop、transcript receipt 和取消传播。
> durable 状态与上下文协议见 [`agent-orchestration-model.md`](agent-orchestration-model.md)。
> Agent 安全策略、execution 安全快照和操作系统隔离见 [`../sandbox.md`](../sandbox.md)。

## 1. 身份与 actor

`Agent` 是持久化身份；`AgentExecution` 是一次持久化执行；`AgentRuntime` 是进程内长期 actor。runtime 不构成新的领域层次，也不能独立创建 AgentExecution。

```text
AgentRuntime(AgentID)
└── active AgentExecution?      同时最多一个
```

同一个 Agent 的 activation、cancel、pause、close 和 settlement 必须串行化。不同 Agent 是否并发由编排策略、能力授权和系统资源决定。

## 2. Activation

只有 SQLite 中已经存在的 `AgentExecution(status=starting)` 才能进入 runtime：

```text
AgentExecution(starting)
    -> 打开 Agent transcript
    -> 校验 SessionContext revision 和 InputSnapshot
    -> 幂等追加 execution_started
    -> 写入 start content（如有）
    -> fsync
    -> 回调 Orchestrator 标记 running
    -> 开始 model/tool loop
```

若 activation 重试，必须根据 `ExecutionID` 和 `RequestID` receipt 判断已有步骤，不重复追加消息或启动第二个 provider stream。

## 3. Execution 输入

runtime 接收不可变 `InputSnapshot`，至少包括：

- `SessionID`、`AgentID` 和 `ExecutionID`；
- 启动时读取的 `ContextRevision`；
- 从固定 SessionContext revision 生成的 `ContextSelection`；
- `ExecutionSecuritySnapshot`；
- 模型、推理档位和 timeout；
- Session 当前工作目录路径快照。

运行中的 runtime 不读取会改变这些边界的配置，也不接受扩大权限或切换上下文 revision 的外部输入。

## 4. Execution loop

```text
preparing
  -> model_streaming
  -> tool_executing
  -> model_streaming ...
  -> settling
```

一次 execution 可以包含多个 model turn、tool call 和 save point。tool executor 必须使用同一个 execution cancellation context，并在 ExecutionSecuritySnapshot 和 approval gate 全部通过后执行。

SessionContext 只在 execution 开始时读取。模型输出、工具调用、工具结果和执行 receipt 追加到所属 Agent 的私有 transcript；runtime 不直接写 SessionContext。

## 5. 暂停与取消

Pause、Close、shutdown 和 execution timeout 都通过 execution context cancellation 传播：

1. Orchestrator durable 记录 control request 或 timeout 状态；
2. runtime 收到取消信号并停止启动新的 model turn 或 tool call；
3. 已在执行的外部操作按其边界返回；
4. runtime 追加 `execution_settled` 并 fsync；
5. Orchestrator 完成产品 settlement。

取消不能删除已写入 transcript 的内容，也不能假装未发生已经完成的文件或外部副作用。

## 6. transcript receipt

Agent transcript 的唯一 writer 负责以下幂等 receipt：

```text
execution_started(ExecutionID, RequestID)
message(..., sourceRequestId?)
execution_settled(ExecutionID, Outcome, FailureCode)
```

receipt durable 后才能通知 Orchestrator 推进对应产品状态。transcript 不接受其他 Agent 的直接写入；共享结论必须经过 SessionContext append command。

## 7. Runtime 不变量

1. runtime 不创建 Project、Session、Agent 或 AgentExecution。
2. runtime 不修改 SessionContext、其他 Agent transcript 或产品审批状态。
3. 每个 Agent 同时最多一个 active execution；每个 execution 只使用一个 runtime actor。
4. `ExecutionID` 是 runtime 与产品编排之间的唯一执行身份。
5. provider stream、tool 临时状态和 cancellation context 在 execution settlement 后释放。
