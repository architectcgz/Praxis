# Praxis 最终架构重构方案

基于 `architectcgz/Praxis` · `codex/execution-coordinator` 分支代码审查及前面确定的设计

## 一、最终结论

采用 SQLite + 会话日志（JSONL）+ Global JSON Config 三层持久化架构。工具执行记录通过 `repository.ToolExecutionRepository` 提供，不新增独立存储层。

同时，取消 `ToolInvocation` 的独立持久化实体，改为根据 JSONL 中的工具调用、审批和执行事件重建状态。

最终明确以下原则：

1. SQLite 保存业务实体：Project、Workspace、Session、Agent、Task、队列、控制意图等。
2. JSONL 保存 Session 历史：User、Assistant、ToolCall、ToolResult，以及工具审批和执行事件。
3. Global JSON 保存全局配置：包括 ApprovalMode。
4. ApprovalMode 只有 Global 和 Project 两级，Session 不保存独立授权模式。
5. ToolInvocation 不再单独持久化。工具调用状态根据 JSONL 中的消息与执行记录重建。
6. 工具启动前必须先持久化 `tool.started`，防止崩溃后重复产生副作用。

这套方案的核心是减少重复事实来源，而不是将现有 JSONL 的所有业务状态原封不动搬进 SQLite。

## 二、最终架构

\#chatgpt-mermaid-\_r_4cf\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:16px;fill:rgb(13, 13, 13);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_4cf\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_4cf\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_4cf\_ .error-icon{fill:rgb(243, 243, 243);}#chatgpt-mermaid-\_r_4cf\_ .error-text{fill:rgb(13, 13, 13);stroke:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_4cf\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_4cf\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_4cf\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_4cf\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_4cf\_ .marker{fill:rgb(143, 143, 143);stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cf\_ .marker.cross{stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cf\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:16px;}#chatgpt-mermaid-\_r_4cf\_ p{margin:0;}#chatgpt-mermaid-\_r_4cf\_ .label{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .cluster-label text{fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .cluster-label span{color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .cluster-label span p{background-color:transparent;}#chatgpt-mermaid-\_r_4cf\_ .label text,#chatgpt-mermaid-\_r_4cf\_ span{fill:rgb(13, 13, 13);color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .node rect,#chatgpt-mermaid-\_r_4cf\_ .node circle,#chatgpt-mermaid-\_r_4cf\_ .node ellipse,#chatgpt-mermaid-\_r_4cf\_ .node polygon,#chatgpt-mermaid-\_r_4cf\_ .node path{fill:rgb(222, 234, 251);stroke:rgb(83, 154, 248);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .rough-node .label text,#chatgpt-mermaid-\_r_4cf\_ .node .label text,#chatgpt-mermaid-\_r_4cf\_ .image-shape .label,#chatgpt-mermaid-\_r_4cf\_ .icon-shape .label{text-anchor:middle;}#chatgpt-mermaid-\_r_4cf\_ .node .katex path{fill:#000;stroke:#000;stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .rough-node .label,#chatgpt-mermaid-\_r_4cf\_ .node .label,#chatgpt-mermaid-\_r_4cf\_ .image-shape .label,#chatgpt-mermaid-\_r_4cf\_ .icon-shape .label{text-align:center;}#chatgpt-mermaid-\_r_4cf\_ .node.clickable{cursor:pointer;}#chatgpt-mermaid-\_r_4cf\_ .root .anchor path{fill:rgb(143, 143, 143)!important;stroke-width:0;stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cf\_ .arrowheadPath{fill:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cf\_ .edgePath .path{stroke:rgb(143, 143, 143);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .flowchart-link{stroke:rgb(143, 143, 143);fill:none;}#chatgpt-mermaid-\_r_4cf\_ .edgeLabel{background-color:rgb(252, 252, 252);text-align:center;}#chatgpt-mermaid-\_r_4cf\_ .edgeLabel p{background-color:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cf\_ .edgeLabel rect{opacity:0.5;background-color:rgb(252, 252, 252);fill:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cf\_ .labelBkg{background-color:rgba(252, 252, 252, 0.5);}#chatgpt-mermaid-\_r_4cf\_ .cluster rect{fill:rgb(243, 243, 243);stroke:rgba(0, 0, 0, 0.1);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .cluster text{fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ .cluster span{color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:12px;background:rgb(243, 243, 243);border:1px solid rgba(0, 0, 0, 0.1);border-radius:2px;pointer-events:none;z-index:100;}#chatgpt-mermaid-\_r_4cf\_ .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cf\_ rect.text{fill:none;stroke-width:0;}#chatgpt-mermaid-\_r_4cf\_ .icon-shape,#chatgpt-mermaid-\_r_4cf\_ .image-shape{background-color:rgb(252, 252, 252);text-align:center;}#chatgpt-mermaid-\_r_4cf\_ .icon-shape p,#chatgpt-mermaid-\_r_4cf\_ .image-shape p{background-color:rgb(252, 252, 252);padding:2px;}#chatgpt-mermaid-\_r_4cf\_ .icon-shape .label rect,#chatgpt-mermaid-\_r_4cf\_ .image-shape .label rect{opacity:0.5;background-color:rgb(252, 252, 252);fill:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cf\_ .label-icon{display:inline-block;height:1em;overflow:visible;vertical-align:-0.125em;}#chatgpt-mermaid-\_r_4cf\_ .node .label-icon path{fill:currentColor;stroke:revert;stroke-width:revert;}#chatgpt-mermaid-\_r_4cf\_ .node .neo-node{stroke:rgb(83, 154, 248);}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_4cf\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_4cf\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node .neo-line path{stroke:rgb(83, 154, 248);filter:none;}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_4cf\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_4cf\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cf\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_4cf\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cf\_ .node text{font-size:14px;font-weight:600;letter-spacing:normal;fill:rgb(0, 79, 153);}#chatgpt-mermaid-\_r_4cf\_ .edgeLabels text{font-size:13px;font-weight:600;letter-spacing:-0.08px;fill:rgb(0, 79, 153);}#chatgpt-mermaid-\_r_4cf\_ .node tspan[font-weight="normal"],#chatgpt-mermaid-\_r_4cf\_ .edgeLabels tspan[font-weight="normal"]{font-weight:600;}#chatgpt-mermaid-\_r_4cf\_ .edgeLabel .label rect{opacity:1;rx:13px;ry:13px;fill:rgb(245, 250, 255);stroke:rgb(206, 219, 229);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .node rect,#chatgpt-mermaid-\_r_4cf\_ .node circle,#chatgpt-mermaid-\_r_4cf\_ .node ellipse,#chatgpt-mermaid-\_r_4cf\_ .node polygon,#chatgpt-mermaid-\_r_4cf\_ .node path{fill:rgb(229, 243, 255);stroke:rgba(0, 0, 0, 0.1);stroke-width:1px;}#chatgpt-mermaid-\_r_4cf\_ .node rect{rx:16px;ry:16px;}#chatgpt-mermaid-\_r_4cf\_ .node.mermaid-decision .label-container{fill:rgb(245, 250, 255);stroke:rgb(206, 219, 229);stroke-dasharray:2,2;}#chatgpt-mermaid-\_r_4cf\_ .edgePaths .flowchart-link{stroke:rgb(143, 143, 143);stroke-width:1px;stroke-linecap:round;stroke-linejoin:round;}#chatgpt-mermaid-\_r_4cf\_ .marker{fill:rgb(143, 143, 143);stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cf\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";}Wails / ReactApplication ServicesSQLite业务实体与 Task 状态JSON Config全局配置Execution CoordinatorReAct LoopToolExecutorCommandPolicy +ApprovalModeJSONL会话消息与工具执行记录Tool State Rebuild

### 数据归属

| 数据                                    | 持久化位置                  |
| ------------------------------------- | ---------------------- |
| Global ApprovalMode                   | `config/settings.json` |
| Project ApprovalMode                  | SQLite `projects`      |
| 模型、凭据、工具配置                            | 现有 JSON 配置文件           |
| Project / Workspace / Session / Agent | SQLite                 |
| Task / Task Queue / 执行控制              | SQLite                 |
| User / Assistant 消息                   | JSONL                  |
| ToolCall 原始参数                         | JSONL                  |
| 工具 Normalize 结果                       | JSONL                  |
| 工具审批记录                                | JSONL                  |
| 工具 Started 事件                         | JSONL                  |
| ToolResult 正文及结果状态                    | JSONL                  |
| ToolInvocation 当前状态                   | 根据 JSONL 重建，不独立持久化      |

JSONL 保存 Session 中的对话消息和工具执行记录，但不负责 Project、Task 等业务实体。消息继续由 `repos.Messages` 读写，工具执行记录与状态恢复通过 `repos.ToolExecutions` 提供，共享现有 Store 的文件、锁、事务和序号。

## 三、JSONL 工具调用设计

### 3.1 消息与工具事件

保留现有 `session.MessageData` 的消息语义，在现有 JSONL 存储中增加工具执行记录。

事件类别建议保持最小集合：

| 类型                        | 作用                              |
| ------------------------- | ------------------------------- |
| `message`                 | User、Assistant、ToolResult 等对话消息 |
| `tool.reserved`           | 某次调用已消耗一次工具额度                   |
| `tool.prepared`           | 冻结 Normalize 后的参数和路径            |
| `tool.approval_requested` | 进入人工审批等待                        |
| `tool.approved`           | 记录用户或策略授权                       |
| `tool.started`            | 工具执行开始凭证已可靠提交                   |

不需要额外的 `tool.completed` 事件。 ToolResult 消息已经表达了执行结果，再追加完成事件会造成重复。

例如，执行成功的工具调用日志可以是：

```
{"type":"message","role":"assistant","task_id":"task_1","tool_calls":[{"id":"call_1","name":"bash","arguments":{"command":"ls"}}]}
{"type":"tool.reserved","task_id":"task_1","call_id":"call_1"}
{"type":"tool.prepared","task_id":"task_1","call_id":"call_1","normalized_arguments":{"command":"ls","path":"E:\\workspace","timeout":120},"path":"E:\\workspace"}
{"type":"tool.approved","task_id":"task_1","call_id":"call_1","source":"yolo","arguments_digest":"sha256:..."}
{"type":"tool.started","task_id":"task_1","call_id":"call_1"}
{"type":"message","role":"tool","task_id":"task_1","call_id":"call_1","status":"succeeded","content":"README.md"}
```

这是目标结构示例，并非当前仓库已有的序列化格式。实际实现需要给事件补充统一的身份、Sequence、Timestamp 和必要的校验字段。

调用身份至少采用 `(SessionID, AgentID, TaskID, CallID)`，并保留 JSONL 中的稳定记录序号。

### 3.2 执行状态如何恢复

工具执行状态恢复

\#chatgpt-mermaid-\_r_4cv\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:16px;fill:rgb(13, 13, 13);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_4cv\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_4cv\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_4cv\_ .error-icon{fill:rgb(243, 243, 243);}#chatgpt-mermaid-\_r_4cv\_ .error-text{fill:rgb(13, 13, 13);stroke:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_4cv\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_4cv\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_4cv\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_4cv\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_4cv\_ .marker{fill:rgb(143, 143, 143);stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cv\_ .marker.cross{stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cv\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:16px;}#chatgpt-mermaid-\_r_4cv\_ p{margin:0;}#chatgpt-mermaid-\_r_4cv\_ .label{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .cluster-label text{fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .cluster-label span{color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .cluster-label span p{background-color:transparent;}#chatgpt-mermaid-\_r_4cv\_ .label text,#chatgpt-mermaid-\_r_4cv\_ span{fill:rgb(13, 13, 13);color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .node rect,#chatgpt-mermaid-\_r_4cv\_ .node circle,#chatgpt-mermaid-\_r_4cv\_ .node ellipse,#chatgpt-mermaid-\_r_4cv\_ .node polygon,#chatgpt-mermaid-\_r_4cv\_ .node path{fill:rgb(222, 234, 251);stroke:rgb(83, 154, 248);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .rough-node .label text,#chatgpt-mermaid-\_r_4cv\_ .node .label text,#chatgpt-mermaid-\_r_4cv\_ .image-shape .label,#chatgpt-mermaid-\_r_4cv\_ .icon-shape .label{text-anchor:middle;}#chatgpt-mermaid-\_r_4cv\_ .node .katex path{fill:#000;stroke:#000;stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .rough-node .label,#chatgpt-mermaid-\_r_4cv\_ .node .label,#chatgpt-mermaid-\_r_4cv\_ .image-shape .label,#chatgpt-mermaid-\_r_4cv\_ .icon-shape .label{text-align:center;}#chatgpt-mermaid-\_r_4cv\_ .node.clickable{cursor:pointer;}#chatgpt-mermaid-\_r_4cv\_ .root .anchor path{fill:rgb(143, 143, 143)!important;stroke-width:0;stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cv\_ .arrowheadPath{fill:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cv\_ .edgePath .path{stroke:rgb(143, 143, 143);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .flowchart-link{stroke:rgb(143, 143, 143);fill:none;}#chatgpt-mermaid-\_r_4cv\_ .edgeLabel{background-color:rgb(252, 252, 252);text-align:center;}#chatgpt-mermaid-\_r_4cv\_ .edgeLabel p{background-color:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cv\_ .edgeLabel rect{opacity:0.5;background-color:rgb(252, 252, 252);fill:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cv\_ .labelBkg{background-color:rgba(252, 252, 252, 0.5);}#chatgpt-mermaid-\_r_4cv\_ .cluster rect{fill:rgb(243, 243, 243);stroke:rgba(0, 0, 0, 0.1);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .cluster text{fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ .cluster span{color:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ div.mermaidTooltip{position:absolute;text-align:center;max-width:200px;padding:2px;font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";font-size:12px;background:rgb(243, 243, 243);border:1px solid rgba(0, 0, 0, 0.1);border-radius:2px;pointer-events:none;z-index:100;}#chatgpt-mermaid-\_r_4cv\_ .flowchartTitleText{text-anchor:middle;font-size:18px;fill:rgb(13, 13, 13);}#chatgpt-mermaid-\_r_4cv\_ rect.text{fill:none;stroke-width:0;}#chatgpt-mermaid-\_r_4cv\_ .icon-shape,#chatgpt-mermaid-\_r_4cv\_ .image-shape{background-color:rgb(252, 252, 252);text-align:center;}#chatgpt-mermaid-\_r_4cv\_ .icon-shape p,#chatgpt-mermaid-\_r_4cv\_ .image-shape p{background-color:rgb(252, 252, 252);padding:2px;}#chatgpt-mermaid-\_r_4cv\_ .icon-shape .label rect,#chatgpt-mermaid-\_r_4cv\_ .image-shape .label rect{opacity:0.5;background-color:rgb(252, 252, 252);fill:rgb(252, 252, 252);}#chatgpt-mermaid-\_r_4cv\_ .label-icon{display:inline-block;height:1em;overflow:visible;vertical-align:-0.125em;}#chatgpt-mermaid-\_r_4cv\_ .node .label-icon path{fill:currentColor;stroke:revert;stroke-width:revert;}#chatgpt-mermaid-\_r_4cv\_ .node .neo-node{stroke:rgb(83, 154, 248);}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_4cv\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_4cv\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node .neo-line path{stroke:rgb(83, 154, 248);filter:none;}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_4cv\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_4cv\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cv\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_4cv\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_4cv\_ .node text{font-size:14px;font-weight:600;letter-spacing:normal;fill:rgb(0, 79, 153);}#chatgpt-mermaid-\_r_4cv\_ .edgeLabels text{font-size:13px;font-weight:600;letter-spacing:-0.08px;fill:rgb(0, 79, 153);}#chatgpt-mermaid-\_r_4cv\_ .node tspan[font-weight="normal"],#chatgpt-mermaid-\_r_4cv\_ .edgeLabels tspan[font-weight="normal"]{font-weight:600;}#chatgpt-mermaid-\_r_4cv\_ .edgeLabel .label rect{opacity:1;rx:13px;ry:13px;fill:rgb(245, 250, 255);stroke:rgb(206, 219, 229);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .node rect,#chatgpt-mermaid-\_r_4cv\_ .node circle,#chatgpt-mermaid-\_r_4cv\_ .node ellipse,#chatgpt-mermaid-\_r_4cv\_ .node polygon,#chatgpt-mermaid-\_r_4cv\_ .node path{fill:rgb(229, 243, 255);stroke:rgba(0, 0, 0, 0.1);stroke-width:1px;}#chatgpt-mermaid-\_r_4cv\_ .node rect{rx:16px;ry:16px;}#chatgpt-mermaid-\_r_4cv\_ .node.mermaid-decision .label-container{fill:rgb(245, 250, 255);stroke:rgb(206, 219, 229);stroke-dasharray:2,2;}#chatgpt-mermaid-\_r_4cv\_ .edgePaths .flowchart-link{stroke:rgb(143, 143, 143);stroke-width:1px;stroke-linecap:round;stroke-linejoin:round;}#chatgpt-mermaid-\_r_4cv\_ .marker{fill:rgb(143, 143, 143);stroke:rgb(143, 143, 143);}#chatgpt-mermaid-\_r_4cv\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,"Segoe UI",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";}Assistant ToolCallnot_startedprepared需要人工审批?awaiting_approvalapprovedToolResult: deniedtool.started有 ToolResult?completed / failedunknown是批准否拒绝有重启后没有

状态重建规则：

| JSONL 中已有的记录          | 重启后状态                   |
| --------------------- | ----------------------- |
| ToolCall，没有 Started   | `not_started`，按已有审批事件继续 |
| 已请求审批，没有审批决定          | `awaiting_approval`     |
| 已批准，没有 Started        | `approved`，执行前重新校验当前策略  |
| Started，没有 ToolResult | `unknown`               |
| 有 ToolResult          | 按消息中的结构化执行状态恢复          |
| 人工拒绝产生的 ToolResult    | `denied`                |

特别强调两点：

第一，没有 `tool.started` 的调用，不需要因为程序重启就标记成 unknown。 它可以重新进入准入、审批或启动过程。

第二，有 `tool.started` 但没有结果，重启后必须视为 unknown，绝不能自动再次执行。

`tool.started` 的语义准确地说是“已经持久化允许进入执行阶段”。如果 Started 刚提交、工具尚未来得及执行就崩溃，保守地判为 unknown 是可以接受的。

### 3.3 started 必须可靠持久化

在现有 Store 上增加工具执行仓储，消息仍由 `repository.MessageStreams` 提供，不重复包装消息 API：

```
type ToolExecutionRepository interface {
    ReserveTool(context.Context, toolexecution.CallKey) (bool, error)
    PrepareTool(context.Context, toolexecution.CallKey, toolexecution.Preparation) error
    RequestToolApproval(context.Context, toolexecution.CallKey) error
    RecordToolApproval(context.Context, toolexecution.CallKey, contracts.ApprovalRecord) error
    TryStartTool(
        context.Context,
        toolexecution.CallKey,
        func(context.Context, toolexecution.State) error,
    ) (bool, error)
    AppendToolResult(
        context.Context,
        toolexecution.CallKey,
        session.MessageData,
        toolexecution.Outcome,
    ) (bool, error)
    GetToolState(context.Context, toolexecution.CallKey) (toolexecution.State, error)
    ListToolStates(context.Context, contracts.SessionID) ([]toolexecution.State, error)
    ToolUsage(context.Context, contracts.SessionID, contracts.TaskID) (ToolUsage, error)
}
```

这是职责示意，最终接口可以按现有 Repository 风格拆成更窄的读写接口。

其中 `TryStartTool` 是关键：

- 在现有 Store 的写入锁下根据历史记录检查当前状态。
- 已有 Started 或 ToolResult 时拒绝再次启动。
- 必须确认审批有效、Task 仍允许执行。
- 追加 Started，并可靠同步到磁盘。
- 只有成功提交且取得启动资格的调用方可以执行工具。

这样不会因为两条并发恢复路径同时发现 `not_started` 而执行两次工具。

同一个调用的状态检查和日志追加必须串行化，不能先查询、释放锁、再另行追加。`TryStartTool` 不接受外层事务 Context，只有自身事务可靠提交后才能返回执行资格；当前 Task、策略和权限由调用方提供的只读授权检查校验。

## 四、ApprovalMode 的最终设计

### 4.1 两级继承

Global：

```
{
  "approvalMode": "ask"
}
```

Project：

```
type Project struct {
    // 原有字段

    ApprovalMode *security.ApprovalMode
}
```

```
type ApprovalMode string

const (
    ApprovalAsk  ApprovalMode = "ask"
    ApprovalAuto ApprovalMode = "auto"
    ApprovalYolo ApprovalMode = "yolo"
    ApprovalDeny ApprovalMode = "deny"
)
```

Project 的 `nil` 代表继承全局，不引入 Session 或 Task 的覆盖配置。全局与项目默认动作只接受 `ask / auto / yolo`，`deny` 用于明确禁止的具体规则。

### 4.2 SQLite Project Schema

```
approval_mode TEXT NULL
CHECK (
    approval_mode IS NULL
    OR approval_mode IN ('ask', 'auto', 'yolo')
)
```

有效授权模式：

\\[ \text{EffectiveMode}= \begin{cases} \text{ProjectMode}, & \text{ProjectMode 非空}\\\ \text{GlobalMode}, & \text{否则} \end{cases} \\]

默认 Global `ask`、Project `NULL`。

Global 更新使用校验后的文件原子替换，不能在磁盘写入失败时提前切换运行时配置。

### 4.3 单次操作的四种处理动作

命令匹配只用于确定本次操作的处理方式，不授予工具或命令模板永久执行权限。统一动作如下：

| 动作 | 行为 |
| --- | --- |
| `ask` | 必须询问用户 |
| `auto` | 模型判断安全后执行，危险则拒绝，不确定则询问用户 |
| `yolo` | 跳过风险审核和人工确认 |
| `deny` | 始终禁止，不能由模型、人工或 yolo 覆盖 |

优先级为：所有命中规则中的 `deny` → 统一 yolo → 配置顺序中的首条具体规则 → 项目默认 → 全局默认。不再配置 Bash 独立的 defaultAction。未命中命令规则的 Bash 和其他工具均使用统一默认动作，不存在隐含的工具放行。

工具被禁用、调用不合法、路径超出允许范围等硬性约束，不受 yolo 覆盖。字符串命令匹配和模型风险审核不提供进程或文件系统沙箱。

独立审核只接收用户任务、冻结参数、工作区和规则来源；没有工具执行能力，不复用 Agent 的系统指令。结果采用 `safe / dangerous / uncertain`，危险直接形成拒绝结果，不进入人工待审；超时、流异常或非法输出按不确定处理。审核请求计入独立请求序号、模型请求数和 token 用量，不消耗 ReAct 迭代，不混入 Agent 对话。

状态转换和授权不变量由 Core 的领域方法维护；调用层只编排事务、模型调用和持久化。审核在存储锁外进行，提交结论与工具启动前均重新读取当前规则。审核记录保存模型身份、调用摘要、结论、简短原因和时间，不保存模型思维过程。

### 4.4 审批事件

`tool.approved` 建议包含：

```
type ToolApproval struct {
    TaskID          contracts.TaskID
    CallID          string
    Source          contracts.ApprovalSource
    ArgumentsDigest string
    ApprovedAt      time.Time
}
```

审批来源：

```
const (
    ApprovalSourceUser = "user"
    ApprovalSourceAuto = "auto"
    ApprovalSourceYolo = "yolo"
)
```

审批摘要使用 Normalize 后的参数，而原始 ToolCall 的参数仍由 Assistant 消息保存。

不应把原始摘要和审批摘要混成同一个概念。

配置动态变化时：

- 从 `auto` 切换到 `ask`，尚未 Started 的旧自动授权失效，需要人工重新审批。
- 已经等待审批的调用，即使切换到 `auto` 或 `yolo`，也不会静默自动批准。
- 任何模式下，当前命令规则为 `deny` 都不得启动。
- 已经 Started 的工具不因配置改变而回滚执行，用户应通过 Stop/Pause 停止。

有必要时可追加 `tool.approval_requested` 来表示重新进入审批，无须把某个已持久化事件改写掉。

## 五、SQLite 的最终职责

SQLite 不再创建 `tool_invocations` 表。

建议数据库包含以下业务对象：

```
praxis.db
├── projects
├── workspaces
├── sessions
├── agents
├── tasks
├── agent_controls
├── session_contexts
├── model_usage
└── operation_timings
```

Task 依然保存冻结的执行配置、任务状态、队列顺序和模型请求预算。

不需要：

```
tool_invocations
message_refs
message_outbox
```

这不是说所有跨存储问题自动消失，而是工具执行状态已经有了一个明确的唯一事实来源：JSONL 会话日志。

### 5.1 工具额度如何处理？

这是现有代码需要一起修改的地方。

当前 `ToolExecutor.reserveCall()` 在同一 JSONL 事务中更新：

```
Task.Progress.ToolCalls++
ToolInvocation.BudgetReserved = true
```

既然不再持久化 ToolInvocation，那么不能直接把 `BudgetReserved` 搬进 SQLite 的另一张表，否则又重新建立了按调用保存的执行状态。

推荐让 `tool.reserved` 成为额度已消耗的唯一事实。

每个 `(TaskID, CallID)` 只能成功追加一次 Reserved：

```
ToolCall
  ↓
检查本次 Task 的已预留工具额度
  ↓
tool.reserved
  ↓
Normalize / Approval / Started
```

工具额度用 JSONL 中属于该 Task 的唯一 Reserved 记录计数。

当前 `MaxToolCalls = 100` 的限制保持不变。

`Task.Progress.ToolCalls` 不再作为 SQLite 的独立持久化计数，可在读取 Task 执行进度时根据执行记录统计填充。

如此一来：

- 工具审批恢复不重复扣额度。
- 相同 CallID 不重复扣额度。
- 多次恢复不会造成双重计数。
- 无需跨 SQLite 和 JSONL 事务同步工具额度。

### 5.2 OutputBytes 如何处理？

当前 `Task.Progress.OutputBytes` 同时包含模型响应输出和工具结果正文的字节数。

为了避免重复计数，建议分开：

- 模型请求和响应消耗继续由 Task 的持久化进度记录。
- ToolResult 字节数由 JSONL 中已提交的工具结果统计。
- 展示或执行限制所需的总输出量，由两部分汇总。

需要调整当前 `Progress` 的字段语义、单调校验与读取适配，不能同时把工具结果字节数保存在 JSONL 和 SQLite，再分别增加一次。

## 六、Task 与 JSONL 的一致性

本方案不需要为了 ToolInvocation 引入跨存储的物理原子事务，但仍然需要 Task 与 JSONL 会话日志的恢复对账。

例如 Assistant 最终消息先写入 JSONL，而 SQLite 的 Task 还没完成结算，进程突然退出。

恢复时应当：

1. 读取 Task 当前状态。
2. 读取该 Task 在 JSONL 中已提交的 Assistant、ToolCall 和 ToolResult。
3. 如果存在可靠的最终 Assistant 完成证据，则避免再次请求模型。
4. 依据当前领域状态转换规则完成 Task 收敛。
5. 如果是 Started 但没有结果，追加 `unknown` 工具回执，而不是重新执行。

这个恢复过程属于 `service/task`，不应该放在 `infra/jsonl` 内部。

JSONL 负责可靠追加、索引和只读查询；SQLite 负责自己的事务和业务实体持久化。

必须承认，两个存储无法天然拥有同一个原子 Commit。因此：

- 跨存储中途失败要阻止继续执行。
- 启动恢复必须先对账，再启动 Coordinator。
- 不得把缺失或损坏的数据静默当成成功。
- 对账要求幂等，不能改变已提交的历史消息。

对于跨存储存在的临时不一致，不必强行用第三张数据表掩盖它。

## 七、当前代码的具体调整

以下是基于现有代码整理的目标目录。`core/tool_invocation` 不再表示一个需要 Repository 保存的实体，其状态转换逻辑可以收敛到 `core/tool_execution`。

```
backend/internal/
├── core/
│   ├── security/
│   │   ├── approval_mode.go
│   │   └── command_policy.go
│   ├── tool_execution/
│   │   ├── state.go
│   │   └── records.go
│   ├── project/
│   ├── session/
│   └── task/
│
├── infra/
│   ├── dataroot/
│   ├── settings/
│   │   ├── config.go
│   │   └── store.go
│   ├── sqlite/
│   │   ├── store.go
│   │   ├── schema.go
│   │   ├── project.go
│   │   ├── session.go
│   │   ├── task.go
│   │   └── ...
│   └── jsonl/
│       ├── store.go
│       ├── messages.go
│       ├── tool_execution.go
│       ├── tool_state.go
│       └── ...
│
├── repository/
│   ├── ...
│
├── service/
│   ├── settings/
│   ├── project/
│   ├── session/
│   ├── task/
│   │   └── recovery.go
│   └── runtime/
│       └── approval.go
│
├── tool_execution/
│   ├── executor.go
│   ├── authorization.go
│   └── state.go
│
├── execution/
├── loop/
└── compose/
```

### 重点改动文件

| 当前文件                                      | 目标改动                                                   |
| ----------------------------------------- | ------------------------------------------------------ |
| `core/tool_invocation/tool_invocation.go` | 移除持久化实体，保留必要的运行时状态模型                                   |
| `core/tool_invocation/update.go`          | 根据执行记录重建状态并校验转换                                   |
| `infra/jsonl/assistant.go`                | Assistant 只提交消息，不再同时创建持久化 ToolInvocation               |
| `infra/jsonl/tool_receipts.go`            | 使用 ToolCall/ToolResult 配对，不依赖 ToolInvocationRepository |
| `infra/jsonl/repositories.go`             | 移除工具与其他业务实体的 JSONL Repository                          |
| `tool_execution/authorization.go`         | 查询当前策略，追加审批事件                                          |
| `tool_execution/state.go`                 | 通过 ToolExecutionRepository 原子提交 Started，不再保存 ToolInvocation            |
| `loop/tools.go`                           | 通过 ToolExecutionRepository 查询重建后的工具状态                                    |
| `loop/resume.go`                          | 从历史消息和工具事件恢复，而不是加载 ToolInvocation                      |
| `service/task/recovery.go`                | 根据 Started/ToolResult 状态完成对账                           |
| `service/runtime/approval.go`             | 通过 ToolExecutionRepository 保存审批结果                                 |
| `compose/application.go`                  | 注入 SQLite、Settings、MessageStreams 和 ToolExecutionRepository                   |

`AGENTS.md` 中关于 JSONL 当前数据结构、ToolInvocation 持久化、预算提交及原子保存的约束，必须与目标架构一致。

## 八、执行顺序

建议按下面的顺序实施，避免一次性修改破坏现有恢复机制。

1. 扩展现有 JSONL 存储

   通过 ToolExecutionRepository 增加工具执行记录、状态恢复、幂等追加和可靠同步能力，同时保持消息 API 的对外语义。
2. 移除持久化 ToolInvocation

   修改 Executor、Loop 和审批服务，使其通过 ToolExecutionRepository 获取状态。人工验证 Started/Unknown 恢复行为。
3. 重构 Task 与工具额度

   将 ToolCalls 消耗归属 JSONL 执行记录，调整 OutputBytes，保留模型请求预算的现有安全约束。
4. 引入 SQLite

   将 Project、Session、Agent、Task 等业务 Repository 从 JSONL 移至 SQLite，不改变业务对象的外部行为。
5. 引入 Global / Project ApprovalMode

   增加 `settings.json`、Project 覆盖配置、Resolver、实时授权校验和前端设置。
6. 恢复、并发与集成验收

   覆盖 Started 前后崩溃、审批恢复、重复请求、工具额度、消息顺序、配置切换和 Coordinator 中断恢复。

JSONL 不设置或检查存储版本标记，只按当前数据结构严格恢复，不提供迁移、兼容读取或静默清理。配置加载拒绝未知字段及不支持的动作，已有配置必须由用户显式调整，不在 Load 或 Reload 中自动改写。

## 九、必须通过的验收条件

| 场景                      | 预期结果                   |
| ----------------------- | ---------------------- |
| ToolCall 已保存，没有 Started | 能重新进入执行准入              |
| 等待用户审批时重启               | 审批等待仍然存在               |
| 用户审批后重启，没有 Started      | 校验当前授权后继续              |
| Started 写入失败            | 工具绝不启动                 |
| Started 成功但执行前崩溃        | 安全地恢复为 unknown         |
| Started 后执行时崩溃          | 不自动重试                  |
| ToolResult 已保存，Task 未结算 | 对账恢复，不重新执行             |
| ToolResult 重复提交         | 不重复追加，不重复计费            |
| 同一调用并发 TryStart         | 最多一次成功获得执行资格           |
| Project 切换 auto → ask   | 尚未 Started 的 auto 授权失效 |
| 命令规则为 deny              | YOLO 也不能绕过             |
| Session 历史读取            | 只返回模型/用户应见的消息，不混入执行事件  |

代码级验证至少包括：

```
cd backend

go build ./...
go vet ./...
bash scripts/check-dependencies.sh
```

不保留测试文件，不新增或运行自动化测试；JSONL 的写入后崩溃、部分写入、`fsync` 失败和进程重启通过人工故障注入验证。

## 最终决定

我建议明确把 Praxis 的存储职责收敛为三个唯一事实来源：

SQLite — 业务事实

Project、Session、Agent、Task、队列及相关配置和执行控制。

JSONL — Session 历史事实

对话消息、ToolCall、审批、Started、ToolResult。工具状态根据日志重建，不再单独持久化。

JSON Config — 全局配置事实

全局 ApprovalMode、模型、工具规则、Agent 定义和凭据配置。

最重要的变化是：彻底取消持久化的 ToolInvocation，根据消息与执行记录重建工具状态，并通过 ToolExecutionRepository 对外提供能力。

这比之前“JSONL 保存 ToolCall，SQLite 再保存 ToolInvocation，然后在两个存储之间同步状态”的方案更加统一。

ToolExecutor 仍负责真实授权和执行；Loop 仍负责 ReAct 流程；SQLite 不感知 ToolCall；JSONL 不感知 Project 和 Task 的业务状态转换。

工具执行记录复用现有 JSONL Store，不单独建立文件写入器、锁或重复的消息读写接口。