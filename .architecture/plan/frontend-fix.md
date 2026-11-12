# 前端代码质量与交互修复计划

## 一、背景

本计划覆盖前端交互正确性、可读性、代码风格与渲染效率，依据当前工作区代码的静态 review 制定。

前端继续采用现有的 `app / features / api / components / styles` 组织方式，保留按会话持有状态、查询与流式输出分别订阅、统一设计令牌及 Wails API 边界。

本次 review 已执行 `npm --prefix frontend run check`，ESLint、TypeScript 与生产构建均通过。尚未进行桌面交互回归或性能测量；焦点、桌面外链和性能相关结论需要在实施时通过人工验证确认。

## 二、目标与范围

### 目标

1. 审批、历史浏览、发送、焦点和异步操作具有完整且可观察的成功与失败路径。
2. 组件与 Hook 的职责清楚，复杂状态推进可直接阅读，不依赖无调用方的逻辑或隐式约定。
3. 流式增量只更新必要的展示区域，不因无关 Context 值变化反复更新历史消息。
4. 在现有工具链内逐步加强类型导入、异步调用和代码风格约束。

### 范围约束

- 本计划文件的编写不代表开始执行；代码修改按后续授权开展。
- 实施前重新检查相关文件及未提交改动，不覆盖工作区已有修改。
- 不修改任何 `AGENTS.md`，不手工修改生成绑定或构建产物。
- 不更换状态管理库、CSS 方案或表单框架，不引入通用请求、菜单、缓存或重试框架。
- 不为假设的竞态增加额外校验、锁或状态层；仅修复可说明触发路径的问题。
- 不新增、保留或运行自动化测试，不自动启动开发服务。
- 验证使用现有静态检查、生产构建和人工回归；需要新启动服务时先征得同意。
- 新增或修改的注释与文档使用中文，不另行创建优化总结文档。

## 三、实施顺序

| 阶段 | 重点 | 完成条件 |
|---|---|---|
| 第一阶段 | 核心交互修复 | 审批失败可见、历史展示独立、多会话焦点正确、发送跟随有效、异步失败有归属、外链使用正确 API |
| 第二阶段 | 可读性与职责整理 | 清理无调用方逻辑、简化状态表达、缩短无意义透传、分离复杂交互职责 |
| 第三阶段 | 渲染与工程约束 | 限制无关 Context 更新、减少重复历史计算、完成针对性测量、小范围补充 lint 与样式边界约束 |

每项保持可独立检查的修改范围。先完成行为修复，再整理结构，避免将功能变化与大规模移动、格式化混在一起。

## 四、第一阶段：核心交互修复

### 1. 审批加载失败的展示与重试

- [x] 修复 `ToolApprovals.tsx` 中无审批项时提前返回、同时隐藏查询错误的路径。
- [x] 区分审批列表为空、查询失败和提交决定失败；不因为没有待审批项而吞掉错误提示。
- [x] 使用现有 `reload` 动作提供明确的重试入口，展示重试中的状态。
- [x] 保持审批决定只在审批闸口操作，不在活动账本增加第二套审批按钮。

**主要文件**

- `frontend/src/features/agent/ToolApprovals.tsx`
- 对应审批区域样式，仅在展示确有需要时修改。

**验收标准**

- 首次查询失败且列表为空时，仍能看到错误及重试入口。
- 重试成功后恢复审批项或明确的空列表状态，错误随成功结果清除。
- 批准、拒绝及其失败提示维持现有业务语义，不误授权其他调用。

### 2. 历史浏览与实时展示分离

- [x] 明确对话处于实时模式还是历史浏览模式，在工作区组合边界确定展示数据。
- [x] 历史消息区域只呈现所选节点的历史范围，不附加当前 Run 的流式内容、等待响应占位或当前协作结果。
- [x] 保留后台事件订阅及实际执行；返回最新位置时直接显示最新状态。
- [x] 如历史浏览期间保留当前运行状态、停止或审批操作，放在明确标识的实时操作区域，不伪装成历史快照的一部分。
- [x] 维持显式创建分支后继续执行的规则，不因为展示重构改变历史写入限制。

**主要文件**

- `frontend/src/features/workspace/SessionWorkspace.tsx`
- `frontend/src/features/agent/AgentSessionPanel.tsx`
- `frontend/src/features/agent/AgentConversation.tsx`
- `frontend/src/features/session/useBranchNavigation.ts`，仅在模式传递需要时修改。

**验收标准**

- 在执行期间浏览旧节点，历史区域不会继续追加当前执行的输出。
- 历史分页与当前分支分页互不混用。
- 返回最新位置后，当前输出、执行状态和必要操作入口完整恢复。
- 已有分支创建与历史提交限制保持不变。

### 3. 多会话实例的 ID 与焦点归属

- [x] 输入框聚焦使用实例所属的 `ref` 或明确的聚焦动作，不使用固定 ID 的全局查找。
- [x] 使用 `useId()` 生成输入框、命令菜单、菜单选项及其他关联控件的实例级 ID。
- [x] 同步调整 `aria-controls`、`aria-activedescendant`、`htmlFor` 等引用。
- [x] `useSlashCommandMenu` 明确依赖所属输入框，外部点击与焦点退出判断不再把其他会话输入框视为自身输入框。
- [x] 分支弹窗关闭后的焦点恢复由所属入口负责，避免与 `Overlay` 的恢复行为相互覆盖。

**主要文件**

- `frontend/src/features/agent/AgentTaskInput.tsx`
- `frontend/src/features/agent/PrimaryAgentTaskInput.tsx`
- `frontend/src/features/agent/SlashCommandMenu.tsx`
- `frontend/src/features/agent/ApprovalModeSelect.tsx`
- `frontend/src/features/session/BranchNavigation.tsx`

**验收标准**

- 同时打开两个以上会话，聚焦按钮及菜单关闭后的焦点均回到当前会话。
- 隐藏会话不会成为全局查找的错误目标。
- 命令候选的键盘导航、选择、Escape 关闭及辅助技术关联正确。

### 4. 发送动作与滚动跟随接通

- [x] 从真实任务提交链路定义“发送成功后恢复跟随”的动作，不依赖没有生产者的乐观消息集合。
- [x] 成功提交后滚动到当前会话的最新位置并恢复跟随；提交失败时保留草稿，不产生成功发送的展示。
- [x] 滚动意图由会话输入与对话组合层协调，避免让任务 API 或存储层直接操作 DOM。
- [x] 明确队列任务与对话消息的差异，不仅为触发滚动而构造额外的乐观消息状态。

**主要文件**

- `frontend/src/features/task/useTaskSubmission.ts`
- `frontend/src/features/workspace/useSessionPage.ts`
- `frontend/src/features/workspace/SessionWorkspace.tsx`
- `frontend/src/features/agent/AgentConversation.tsx`

**验收标准**

- 用户滚离底部后成功发送任务，会回到最新位置并继续跟随输出。
- 仅接收后台增量时，不打断用户主动浏览历史。
- 加载更早历史不会被错误识别为新发送。
- 提交失败不清除草稿，不显示不存在的成功消息。

### 5. 明确异步失败处理责任

- [x] 检查刷新、配置重载事件、目录清理重试等可能 reject 的入口，确认每个终端调用方负责处理失败。
- [x] 需要向上层传播失败的查询继续保留抛错能力；用户操作入口负责错误展示及 pending 收尾。
- [x] 不将 `void promise` 当作错误处理，不用统一空 `catch` 隐藏失败。
- [x] 目录清理重试展示进行中与失败状态，失败后仍保留重试目标。
- [x] 错误恢复时只清理由对应操作拥有的错误，不顺带抹除无关错误。

**主要文件**

- `frontend/src/features/setting/model-config/useModelCatalog.ts`
- `frontend/src/features/workspace/useSessionPage.ts`
- `frontend/src/features/workspace/ProjectWorkspace.tsx`
- `frontend/src/features/workspace/SessionWorkspace.tsx`
- `frontend/src/features/session/useBranchNavigation.ts`

**验收标准**

- 手动刷新、配置重载通知及目录清理重试失败时，不出现未处理的 Promise rejection。
- 用户可看到失败原因，操作状态能够结束，并能再次尝试。
- 需要根据失败中止后续流程的调用方仍可准确获知失败。

### 6. 统一桌面外链调用

- [x] 桌面环境通过已依赖的 `@wailsio/runtime` 使用 `Browser.OpenURL()`。
- [x] 普通浏览器预览使用 `window.open()`，保留协议限制及不替换当前页面的语义。
- [x] 处理桌面外链调用的异步失败，不静默丢弃 rejection。
- [ ] 在桌面环境人工确认实际打开行为，不将浏览器预览结果当作桌面验证。

**主要文件**

- `frontend/src/api/filePreview.ts`
- `frontend/src/features/agent/output/MarkdownViews.tsx`

**验收标准**

- 桌面环境的网页链接通过系统浏览器打开，不替换当前会话。
- 浏览器预览仍能在新标签页打开允许的网页链接。
- 文件预览链接和被禁止的协议保持现有边界。

## 五、第二阶段：可读性与职责整理

### 7. 清理无调用方状态与不可达分支

- [x] 在发送与滚动方案确定后，重新检查 `beginInput / acceptInput / rejectInput`、`pendingUserMessages` 及其类型、透传参数、展示组件的实际用途。
- [x] 对确实不再承担业务职责的完整调用链进行删除，不保留仅为兼容内部代码而存在的空接口。
- [x] 检查 `agentStore.receive()` 已提前处理的终态事件与 `mergeStreamingEvent()` 的责任，确保同一事件只有清楚的状态推进位置。
- [x] 删除或修改代码时同步修正相关注释，不保留与实现不一致的行为描述。

**主要文件**

- `frontend/src/features/agent/agentStore.ts`
- `frontend/src/features/agent/useAgentData.ts`
- `frontend/src/features/agent/streaming.ts`
- `frontend/src/features/agent/types.ts`
- 相应消费组件及导出入口。

**验收标准**

- 保留的状态和动作均有明确生产者、消费者及生命周期。
- 流式到持久化条目的衔接、结束清理与历史分页行为不改变。

### 8. 展开复杂状态推进与简化状态表示

- [x] 将 `agentStore.loaded()` 的条目合并、分页更新和输出清理按语义分开表达。
- [x] 将多步状态更新、嵌套三元表达式及压缩的异常处理展开为易核对的正常流程。
- [x] 优先使用具名局部变量，不为单次简单表达式机械抽函数。
- [x] 输入区四类互斥浮层使用单一联合状态，例如 `provider | model | reasoning | approval | null`。
- [x] 模型表单错误使用字段身份和文案表达，不以中文错误字符串作为逻辑判别条件。

**主要文件**

- `frontend/src/features/agent/agentStore.ts`
- `frontend/src/features/agent/AgentTaskInput.tsx`
- `frontend/src/features/session/useBranchNavigation.ts`
- `frontend/src/features/workspace/useWorkspaceCommands.ts`
- `frontend/src/features/setting/model-config/ModelDialog.tsx`

**验收标准**

- 读者可以直接判断状态合并与错误收尾的执行顺序。
- 同时只能打开一个输入选择器的约束由状态表示保证。
- 修改错误文案不影响字段错误定位。

### 9. 按真实职责拆分复杂组件

- [x] 将输入选择器的定位、焦点与监听生命周期封装为就近的明确职责，不扩展成通用浮层框架。
- [x] 将会话操作菜单从项目列表的主要展示流程中分离，保留清楚的删除、重命名与定位文件回调。
- [x] 模型表单校验形成可直接阅读的独立逻辑，表单主体保留正常提交流程。
- [x] 评估由组合层传入输入区插槽，减少 `SessionWorkspace → AgentSessionPanel → AgentConversation → AgentTaskInput` 中无业务作用的参数透传。
- [x] 不把整个页面状态对象传给底层组件，不为减少参数数量扩大依赖面。

**主要文件**

- `frontend/src/features/project/ProjectsPanel.tsx`
- `frontend/src/features/agent/AgentTaskInput.tsx`
- `frontend/src/features/agent/AgentSessionPanel.tsx`
- `frontend/src/features/agent/AgentConversation.tsx`
- `frontend/src/features/workspace/SessionWorkspace.tsx`
- `frontend/src/features/setting/model-config/ModelDialog.tsx`

**验收标准**

- 列表展示、输入编辑和复杂浮层生命周期可以分别阅读与修改。
- 菜单定位、外部点击、Escape、Tab、箭头导航和焦点恢复保持完整。
- 新增文件仅对应明确职责，不以固定行数作为拆分指标。

### 10. 就近复用重复逻辑

- [x] 统一消息时间格式化，保留原有本地化展示与无效时间处理；唯一实现由历史消息展示持有。
- [x] 统一消息与代码块复制所使用的重复剪贴板逻辑，明确失败反馈。
- [x] 共享逻辑留在 `agent/output` 等实际拥有其语义的位置，不创建泛化工具集合。

**主要文件**

- `frontend/src/features/agent/output/EntryView.tsx`
- `frontend/src/features/agent/output/StreamingViews.tsx`
- `frontend/src/features/agent/output/MarkdownViews.tsx`

## 六、第三阶段：渲染与工程约束

### 11. 收窄流式更新的渲染范围

- [ ] 稳定用量与审批 Context value，仅在实际数据或操作依赖变化时通知消费者。
- [ ] 对 Context 中确实需要稳定引用的动作维护正确依赖，不机械为全部函数添加 `useCallback`。
- [ ] 将仅依赖 `entries` 的历史分组、复制文本与 Turn 集合计算从每次文本增量路径中分离。
- [ ] 整理稳定历史区域与实时区域的渲染边界，保留会话后台数据接纳能力。
- [ ] 通过已有环境的 Profiler 人工观察长历史及多 Agent 输出，记录哪些组件仍在每次 delta 更新。
- [ ] 仅在测量证明有收益时，增加用量查询索引等局部优化；不预先引入虚拟列表或全局缓存。

**主要文件**

- `frontend/src/features/agent/AgentSessionPanel.tsx`
- `frontend/src/features/agent/AgentConversation.tsx`
- `frontend/src/features/agent/SessionTokenUsage.tsx`
- `frontend/src/features/agent/ToolApprovals.tsx`
- `frontend/src/features/agent/output/MessageTiming.tsx`

**验收标准**

- 用量与审批数据未变化时，纯文本增量不因 Provider value identity 改变而通知全部相关消费者。
- 历史派生数据不在每次文本增量时重新完整计算。
- 输出顺序、工具状态、耗时、用量及复制内容保持正确。
- 性能结论以实际观察为依据，不以代码变化推断性能提升幅度。

### 12. 增强现有静态检查与类型风格

- [ ] 统一本次涉及位置的 `import type` 或具名 `type` 导入，遵守现有 React 类型写法。
- [ ] 使用已安装的 `typescript-eslint` 评估类型导入一致性及类型感知 Promise 检查。
- [ ] 明确 Promise 规则对 `void` 的处理，不能因规则默认忽略 `void` 就认为失败路径已覆盖。
- [ ] 先检查规则影响范围，再启用少量有明确价值的规则，不一次性开启全部规则或格式化全仓。
- [ ] 保留现有 `npm run lint` 与 `npm run check` 入口，不新增平行验证工具链。

**主要文件**

- `frontend/eslint.config.js`
- 本次修改涉及的 TypeScript 文件。

**验收标准**

- 静态检查能约束约定范围内的类型导入与异步使用。
- 无为通过检查而新增的无理由禁用注释或空异常捕获。

### 13. 明确样式规则所有权

- [ ] 将主题文件中混入的几何布局属性归到相应布局或功能样式文件。
- [ ] 保持主题文件主要负责颜色、边框、阴影与材质，减少同一属性在多个文件间覆盖。
- [ ] 保持全局 token、样式入口顺序、响应式断点及减少动效规则。
- [ ] 仅整理与本次工作相关且可验证的规则，不批量改名或迁移 CSS 方案。

**主要文件**

- `frontend/src/styles/workspace.css`
- `frontend/src/styles/workspace/theme-navigation.css`
- `frontend/src/features/project/theme.css`
- 对应布局与功能样式文件。

**验收标准**

- 样式调整不改变预期尺寸、滚动所有者、裁剪与浮层层级。
- 白天、夜间及适用响应式状态的计算样式保持正确。

## 七、验证方式

第一、二阶段的实现复选框仅表示代码修改完成，不替代交互验收。当前 `npm --prefix frontend run check` 已通过；审批失败与重试、执行中历史浏览及分页、多会话焦点与键盘操作、发送后的滚动跟随、配置刷新与目录清理失败、桌面外链、会话操作菜单、模型字段错误及剪贴板失败反馈等人工回归仍待在运行环境中验证。本轮未启动服务、未新增或运行自动化测试。

### 静态检查与生产构建

每个阶段完成后执行：

```bash
npm --prefix frontend run check
```

该命令包含 ESLint、TypeScript 和生产构建，并按现有脚本将构建资源同步到 `backend/frontend/dist`。构建产物由脚本生成，不手工修改。

本计划原则上不涉及 Wails binding 签名变更。如实际实施必须改变签名，先确认额外范围，再按仓库流程重新生成绑定。

### 人工回归清单

| 场景 | 检查重点 |
|---|---|
| 首次审批查询失败 | 错误可见、可重试、成功后恢复审批入口 |
| 批准与拒绝失败 | pending 正确收尾，失败反馈属于对应操作 |
| 执行中浏览历史 | 历史不混入实时输出，返回最新后数据完整 |
| 历史分页 | 页码身份、条目顺序、加载状态与当前分支隔离 |
| 两个以上会话切换 | 当前输入聚焦、隐藏页面不干扰、ARIA 关联唯一 |
| 命令与选择器 | Escape、Tab、方向键、外部点击、焦点恢复 |
| 滚离底部后发送 | 成功恢复跟随，失败保留草稿，后台增量不抢滚动 |
| 配置刷新与清理失败 | 无未处理 rejection，错误可见，可再次操作 |
| 网页与文件链接 | 桌面使用系统浏览器，文件仍走预览，禁止协议不可打开 |
| 长历史与多 Agent 输出 | 观察实际渲染范围，核对复制、计时、用量与工具状态 |
| 样式变更 | 双主题、适用断点、短窗口、浮层、滚动与减少动效 |

失败场景通过已有环境中可控的操作条件人工验证，不新增自动化测试、不在业务代码中加入测试专用逻辑。没有可用运行环境时，明确记录未验证项，不将静态检查等同于交互验收。

## 八、风险与回滚

| 风险 | 控制方式 |
|---|---|
| 历史与实时模式调整使停止或审批入口不可达 | 明确消息展示与实时操作的归属，在执行中浏览历史的场景单独回归 |
| 焦点改动破坏嵌套浮层或键盘操作 | 保留 `Overlay` 的焦点与层级职责，逐个入口核对恢复目标 |
| 清理状态时误删仍有业务用途的路径 | 先完成真实提交与输出调用链核对，再整体清理生产者、消费者及类型 |
| memo 化遗漏依赖导致展示不更新 | 仅针对已确认的更新路径优化，保留实际响应式依赖并执行 Hooks 检查 |
| CSS 属性移动改变层叠效果 | 小范围迁移，核对加载顺序、计算样式、断点和状态选择器 |
| 新 lint 规则扩大无关修改 | 先评估报告，必要时缩小规则范围或拆分实施，不压入行为修复 |

回滚以单项或单阶段为单位，只撤回本次对应修改，不使用会清除工作区其他未提交内容的整体重置。涉及样式顺序、状态接口或组件拆分时，应将其关联调用方作为同一回滚单元。

## 九、完成标准

- [ ] 第一阶段各项行为验收完成，或明确列出缺少环境而未验证的项目。
- [x] 无调用方逻辑已按实际业务用途清理，状态与异常处理责任明确。
- [x] 结构调整未引入仅用于转发的层级或超出需求的通用抽象。
- [ ] 性能优化有实际渲染观察依据，不以猜测作为扩展范围的理由。
- [x] `npm --prefix frontend run check` 通过。
- [ ] 人工回归覆盖本次变更涉及的会话、输入、审批、历史与样式状态。
- [ ] 未修改 `AGENTS.md`，未手工修改生成文件，未新增或运行自动化测试，未擅自启动服务。
