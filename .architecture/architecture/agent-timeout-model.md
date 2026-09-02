# Agent 命令与执行超时模型

> 本文定义 command acknowledgement、AgentExecution、上下文提交、暂停和关闭的 timeout 语义。
> 编排时序见 [`agent-orchestration-model.md`](agent-orchestration-model.md)，runtime 边界见 [`agent-runtime-model.md`](agent-runtime-model.md)。

## 1. 超时边界

```text
Command acknowledgement timeout
  -> 等待 durable command transaction

Execution timeout
  -> 单个 AgentExecution 的总时长、model call 和 tool call

Context commit timeout
  -> 等待 SessionContext append transaction

Stop timeout
  -> Pause / Close 后等待 cancellation 和 durable settlement

ManagedProcess stop timeout
  -> 等待 graceful shutdown，随后关闭该进程的 Job Object
```

调用方等待超时只影响响应，不代表 command 未提交。任何已经 durable 的 execution、control request 或 context entry 都必须继续收敛。

## 2. Command acknowledgement

`SendInput`、`Resume`、咨询启动和 Context commit 的同步响应只确认对应的 durable transaction：

```text
调用方 -> Orchestrator transaction -> commit -> accepted
                                  \\-> reject with stable error
```

调用方超时后使用同一个 `RequestID` 或 Context commit identity 查询和重试。服务端必须返回既有结果，不能创建第二个 execution 或重复追加 ContextEntry。

### 2.1 前端确认重试

前端只对传输失败或 command acknowledgement 超时执行自动重试。一次用户意图在第一次发送前生成一个 `RequestID`，后续尝试复用该 ID、完全相同的参数和同一个命令类型。

- 首次请求之外最多自动重试 3 次，共 4 次传输尝试；重试之间使用有界退避。
- 自动重试期间显示“正在重试”状态，不把超时直接显示为命令失败。
- 达到预算后显示“结果未知/等待确认”状态，并提供查询或手动重试操作；手动操作仍复用原 `RequestID`，不能生成第二个业务意图。
- 收到服务端明确的校验、权限、revision conflict、模型未配置或其他稳定业务错误时，不自动重试。

前端的重试计数、退避和展示属于客户端命令调用层。应用层验证 `RequestID` 与参数摘要并返回既有回执；`core/command` 仅定义身份和回执协议，不持有 UI 重试状态。

## 3. Execution deadline

deadline 绑定 `AgentExecution`，不能只配置在长期 AgentRuntime 上。它描述一次已经接受的执行，不描述前端传输尝试：

- model stream 接收 execution context；
- tool call 使用相同 context 或更短的 child deadline；
- child deadline 不能延长 parent execution 的更短 deadline；
- 到期后停止后续 turn，使用稳定 failure code settlement；
- 已 durable 的 transcript 和已发生副作用不能回滚。

Provider/model 请求超时属于 execution 的运行时结果。前端不得因为这类超时再次发送同一 `SendInput`；应查询 execution 投影，并按产品规则提供 Resume 或显式的新执行。显式业务重试由 application service 创建新的 `AgentExecution`，使用新的 `ExecutionID` 和新的业务 `RequestID`。

## 4. Context commit timeout

SessionContext append 是单事务 compare-and-append：

```text
AppendContext(SessionID, ExpectedRevision, Entry)
  -> expected revision matches current revision
  -> append new entry and increment revision
```

如果调用方超时，使用同一提交身份查询结果。若 revision 已被其他人推进，重试必须返回冲突，不能覆盖后来内容。咨询结论只有在提交成功后才对后续 execution 可见。

## 5. Pause、Close 与 shutdown

```text
Pause / Close
  -> durable control request
  -> cancel current execution context
  -> 等待 provider/tool 响应取消
  -> flush transcript settlement
  -> product settlement transaction
```

调用方 context 先到期时，后台仍使用独立的 bounded settlement context 完成取消和结算。不能直接关闭 event channel、丢弃 active execution 或删除控制请求。

Agent Pause、Close 和 AgentExecution settlement 不停止 Session 拥有的 ManagedProcess。`StopManagedProcess`、Session close 和 application shutdown 使用独立的 stop deadline：先发送 Ctrl+C 或工具声明的 graceful signal；到期后关闭 ManagedProcess Job Object，并继续完成 durable settlement。

## 6. Durable timeout 结果

| 超时对象 | 保留事实 | recovery 行为 |
|---|---|---|
| command acknowledgement | `RequestID`、目标身份和已提交状态 | 查询既有 command 结果 |
| AgentExecution | `ExecutionID`、状态、上下文 revision 和 failure code | 继续 activation 或收敛为 interrupted |
| Context commit | SessionID、提交 identity、expected/current revision | 返回已提交结果或 revision conflict |
| Pause / Close | control request、目标 execution 和 applied 状态 | 继续 cancellation / settlement |
| ManagedProcess stop | ManagedProcessID、stop request、状态和来源 execution | 关闭残留 runtime 并收敛为 settled 或 interrupted |
| shutdown | 未结算 execution 和资源 owner | 下次启动先 recovery，再开放命令 |

## 7. 不变量

1. 调用方等待超时不撤销 durable command。
2. 前端自动重试只覆盖传输/确认超时，并复用同一个 `RequestID`；它不能触发第二个 execution。
3. 子调用不能延长 parent execution 的更短 deadline。
4. execution/provider timeout 必须结算为 execution 状态，不能由前端重复提交输入来恢复。
5. timeout 不能抹除已 durable 的 transcript、SessionContext 或审计事件。
6. Pause、Close 和 shutdown 的 settlement 使用独立于被取消 execution 的有界上下文。
7. ManagedProcess 的 graceful stop 超时后必须关闭其 Job Object；调用方等待超时不能让 Supervisor 放弃回收。
