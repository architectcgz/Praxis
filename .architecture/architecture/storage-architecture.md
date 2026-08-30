# Praxis 存储架构

> 本文定义 Project、Session、SessionContext、Agent transcript、AgentExecution、ManagedProcess 和文件数据的事实源及恢复边界。
> 领域归属见 [`structure.md`](structure.md)，编排恢复时序见 [`agent-orchestration-model.md`](agent-orchestration-model.md)。
> AgentSecurityPolicy、ExecutionSecuritySnapshot 和 ToolInvocation 的安全语义见 [`../sandbox.md`](../sandbox.md)。

## 1. 事实源分工

| 存储 | 保存 | 不保存 |
|---|---|---|
| SQLite | Project、Session、SessionContextEntry、Agent、AgentSecurityPolicy、AgentExecution、ExecutionSecuritySnapshot、ToolInvocation、ManagedProcess、WorkflowDefinition、WorkflowInstance、WorkflowNodeInstance、WaitCondition、ControlRequest、Result、Briefing、Note 元数据和审计事件 | Agent transcript 正文、provider 逐 delta 流、ManagedProcess 实时终端输出 |
| per-Agent JSONL | 该 Agent 实际看过和产生的 user、assistant、tool message、`execution_started`、`execution_settled` receipt | 其他 Agent transcript、未提交 SessionContext、pending command |
| 文件树 | attachments、过长 tool output、patch、Note 正文和 Session 工作目录引用的外部文件 | 产品状态机和关系身份 |

SQLite 是编排事实源；Agent JSONL 是私有交互事实源；文件树只承载大对象或外部工作目录内容。三者不重复拥有同一状态机。

## 2. SQLite 关系

目标数据库至少表达以下关系：

```text
projects
sessions(project_id)
session_context_entries(session_id, revision)
agents(session_id)
agent_security_policies(agent_id, revision)
agent_executions(agent_id, session_id, context_revision)
execution_security_snapshots(execution_id, agent_policy_revision)
tool_invocations(execution_id, invocation_id)
managed_processes(session_id, started_by_execution_id, started_by_invocation_id)
wait_conditions(session_id, agent_id, created_by_execution_id)
agent_results / briefings / notes
agent_control_requests
orchestration_events
workflow_definitions / workflow_instances / workflow_node_instances /
workflow_node_executions
```

`session_context_entries` 对 `(session_id, revision)` 唯一；写入只能在事务中检查当前 revision 并追加下一版本。`agent_executions` 对 `ExecutionID` 唯一，并保存启动时固定的 `ContextRevision`。每个 execution 恰好拥有一份不可变的 `execution_security_snapshots` 记录。

Project 路径用于展示和执行输入，不作为外键或关系身份。文件操作执行时根据 ExecutionSecuritySnapshot 中已解析的路径范围校验实际访问。

## 3. Agent transcript

每个 Agent 一份 JSONL 文件，路径由 `SessionID` 和 `AgentID` 的稳定身份派生。首条记录是不可变 transcript header，后续记录按序追加：

```text
session_header
execution_started
message
context_artifact?       仅用于该 Agent 已实际应用的结构化内容
execution_settled
```

咨询 Agent 的完整问题、回答和中间交互留在其私有文件。将咨询结论共享给 Session 时，SQLite 追加 `SessionContextEntry`；不能通过直接复制 transcript 实现共享。

## 4. 跨存储一致性

SQLite 与 JSONL 不共享事务。一致性依靠稳定身份、receipt 和 compare-and-append：

| 场景 | 幂等身份 | 对账方式 |
|---|---|---|
| execution start | `RequestID` + `ExecutionID` | `execution_started` 与 source request receipt |
| execution settlement | `ExecutionID` | `execution_settled` receipt |
| ManagedProcess start | `StartedByInvocationID` + `ManagedProcessID` | ToolInvocation result 与 ManagedProcess 状态 |
| ManagedProcess settlement | `ManagedProcessID` | stop request、Supervisor exit report 与 settled 状态 |
| SessionContext append | 提交 identity + `ExpectedRevision` | 已提交 revision 或 revision conflict |
| Agent message | `sourceRequestId` 或消息 identity | transcript sequence 与 JSONL 重复检查 |

不使用伪造的跨文件共享事务。任何跨存储操作都必须允许 recovery 继续重试或收敛。

## 5. DataRoot

```text
{DataRoot}/
├── config/                    Provider、model、Profile 和策略配置
├── secrets/                   本地密钥，权限 0600
├── runtime/
│   ├── praxis.db              SQLite 编排事实
│   ├── agent-sessions/        per-Agent JSONL transcript
│   ├── attachments/           大附件与工具输出
│   ├── notes/                 Note 正文
│   └── tmp/                   未完成写入与临时文件
└── projects/                  可选的项目级非敏感文件
```

项目源目录和 Session 工作目录由用户选择，可以位于 DataRoot 外部。DataRoot 只保存它们的路径引用、状态快照和应用自身数据，不复制或接管项目目录的所有权。

## 6. 启动恢复

```text
1. readiness = false，拒绝新 command
2. 修复所有 Agent JSONL partial tail
3. 对账 execution_started / message / execution_settled receipt
4. 检查 SessionContext revision 连续性
5. 将未结算 ManagedProcess 标记 interrupted，并清理对应 AppContainer profile 与 ACL
6. 收敛 execution、control、wait 和 Agent 状态
7. 对无 settlement receipt 的 active execution 标记 interrupted
8. 重新激活 starting execution
9. 所有关键扫描成功后 readiness = true
```

Recovery 不加载全部 transcript 到内存猜测产品状态，也不重放结果未知的模型调用、工具副作用、ManagedProcess 或外部请求。SessionContext 的缺失或 revision 间断是启动错误，不能静默跳过。

## 7. 存储不变量

1. SQLite 是 Project、Session、SessionContext、Agent、AgentExecution 和 ManagedProcess 的唯一产品事实源。
2. 每个 Agent transcript 只有一个 writer；runtime 不能写其他 Agent 的 transcript。
3. SessionContext 只追加不覆盖；后续执行只能读取某个已存在的 revision。
4. 稳定 ID 是关系和恢复的唯一身份；路径和显示名称不能替代 ID。
5. 密钥不进入 Project 文件、SQLite、JSONL、日志或 binding DTO。
6. ManagedProcess 的 PID、ConPTY handle、Job Object 和实时输出不是持久化事实；应用重启后未结算记录统一收敛为 interrupted。
