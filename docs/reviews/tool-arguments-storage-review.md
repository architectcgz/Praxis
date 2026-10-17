# 工具参数持久化专项评审

## 结论

针对 `.architecture/plan/a.md` §3.1、§4.4，建议保留 Assistant ToolCall 的原始 `arguments` 和 `tool.prepared` 的冻结执行参数。两者分别表达模型请求与实际执行输入，不是同一事实的两个独立来源。

本次仅核对参数存储，不修改方案或代码。参考 `code-review@claude-plugins-official` 的证据核验原则；本任务不是 PR，不执行其 GitHub 评论及多 Agent PR 流程。

## 风险评估

1. **问题级别：高（若删除冻结执行参数）**
2. **问题描述：** Normalize 不只是 JSON 格式整理，还会补默认值、解析相对路径。恢复时重新 Normalize 不天然等价于复用审批时的输入。
3. **影响范围：** 若工具默认值或 Normalize 实现变化，重新计算可能与原审批摘要不一致；正确校验应阻止启动，不能静默采用新参数。只保存摘要无法还原原执行输入。
4. **修正建议：** 原始参数仅随 Assistant 消息保存；冻结执行参数仅随 Prepared 保存。审批绑定工具名与冻结参数的摘要，执行和审批恢复都复用冻结参数，不重新 Normalize。Started 只引用调用身份，无须再复制参数。
5. **可选方案：** 在消息提交前就规范化并只保存 canonical ToolCall，但这会改变消息保真语义及非法参数回执流程，不是当前方案下的最小改动，不建议仅为节省一份参数采用。

## 代码依据

- `backend/internal/tools/bash/normalizer.go:64-90`：为 `{"command":"ls"}` 补充工作目录及默认 `timeout=120`，将工作目录解析为绝对路径。
- `backend/internal/tools/apply_patch/normalizer.go:56-72`：保留补丁正文，但补充并解析执行根目录。因此正文可能重复，完整执行输入仍有独立含义。
- `backend/internal/tool_execution/state.go:104-106`：启动时检查审批摘要是否匹配冻结参数。
- `backend/internal/tool_execution/executor.go:135-170`：已准备调用复用持久化的规范化参数，并以这些参数执行。

## 最小调整建议

- 修正方案 §3.1 的示例：Bash 的 `normalized_arguments` 应体现冻结后的工作目录和超时，避免看起来只是重复 `{"command":"ls"}`。
- 当前方案的 Approved 只存摘要、Started 只存调用引用，保持即可。
- 若 `tool.prepared.path` 始终等于规范化参数中的 `path`，可只持久化参数中的路径，投影时派生独立字段；这与删除整个冻结参数快照不同。
- 暂不增加“参数相同则省略”的分支或通用去重存储；只有确认大补丁重复存储成为实际瓶颈时再考虑。
