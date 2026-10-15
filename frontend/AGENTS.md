# Frontend Guidelines

## React 版本与写法

- React 与 React DOM 使用 `~19.2.8`，lockfile 当前解析为 `19.2.8`；两者实际版本必须一致。`@types/react` 使用 `~19.2.18`，`@types/react-dom` 使用 `~19.2.4`，类型定义与运行时保持在 19.2 系列。
- 使用当前版本的稳定 API；引入 API 前确认运行时与类型定义均支持，并同步维护 `package.json` 和 `package-lock.json`。使用 `npm ci` 按 lockfile 安装，验证使用 `npm run check`（Hooks lint 与生产构建）。
- 使用函数组件、Hooks 和自动 JSX transform（`jsx: react-jsx`）；类型使用 `import type` 或具名导入中的 `type`。函数组件用参数默认值，表单提交事件使用 `SubmitEvent<HTMLFormElement>`。
- Context Provider 使用 `<SomeContext value={value}>`；函数组件接收 ref 时直接声明 ref prop。渲染入口使用 `createRoot`，不引入 `ReactDOM.render`、`findDOMNode`、字符串 ref 或函数组件的 `defaultProps` / `propTypes`。
- 错误边界可以使用包含 `getDerivedStateFromError` / `componentDidCatch` 的 class；`memo`、`useMemo`、`useCallback` 按实际需要使用，不因 React 版本升级机械移除，也不假定已启用 React Compiler。

## Hooks、ref 与异步回调

- Hooks 只在组件或自定义 Hook 顶层调用；渲染和 state updater 保持纯函数。渲染期间不读写 `ref.current`，仅允许结果可预测且只执行一次的惰性初始化。
- 影响展示或 Effect 重跑的值使用 state / props；Effect 与 callback 的依赖列出实际读取的响应式值，不把 `ref.current` 当作响应式依赖，也不通过隐藏依赖维持闭包。
- 请求代次、即时选择和异步缓存可以保存在 ref；由对应操作入口同步更新，随后更新展示 state。保留过期响应检查，不能把这些即时更新统一延迟到 Effect，也不能在 state updater 内修改 ref。
- 确实需要异步回调读取最新的已提交状态时，可在 Effect 中同步 ref；必须在浏览器绘制或事件处理前完成同步时使用 `useLayoutEffect`。不要在每次渲染时复制 state 到 ref。
- Effect 注册的定时器、订阅和原生事件回调，需要读取最新值而无需重建连接时使用 `useEffectEvent`。Effect Event 仅由同组件的 Effect 及其注册回调调用，不放进依赖数组，不传给其他组件或 Hook，不用于普通 JSX 事件处理，也不用来跳过应当响应的依赖。
- 普通点击、输入、提交等 JSX 事件直接使用当前渲染的 handler；跨 Hook 传递明确的 callback，并维护 `useCallback` 的依赖，不用 callback ref 间接转发。
- Effect 创建的订阅、监听和定时器必须在 cleanup 中释放，支持 Strict Mode 的重复 setup / cleanup。定时器依赖触发计时的业务值，最新回调用 Effect Event 读取，避免仅因回调 identity 改变而重新计时。

```tsx
const dismiss = useEffectEvent(onDismiss)

useEffect(() => {
    if (!message) return
    const timer = window.setTimeout(() => dismiss(), 3000)
    return () => window.clearTimeout(timer)
}, [message])

// JSX 事件直接使用当前 handler。
return <button onClick={onDismiss}>关闭</button>
```

## Design Guides

- [`STYLE.md`](STYLE.md) 定义视觉风格、设计令牌和组件外观。开始视觉或组件样式任务前必须先读取。
- [`LAYOUT.md`](LAYOUT.md) 定义页面容器、模块边界、尺寸与滚动职责、`overflow`、层叠上下文、浮层和响应式布局。开始页面结构或布局层级任务前必须先读取。
- 同时涉及视觉和布局的前端任务，必须同时遵守两份规范。

## Icons

- 默认使用项目已依赖的 `lucide-react` 图标；新增图标前先检查 Lucide 是否已有对应语义。
- 不要使用 `>`, `v`, `->`, `+`, `x` 等文字或 ASCII 字符充当交互图标。使用 Lucide 图标表达展开、关闭、添加、返回等操作。
- 仅图标按钮必须提供清晰的 `aria-label`，并为不熟悉的图标提供 `title` 工具提示。
- 图标尺寸、线宽和颜色应与相邻控件保持一致，优先复用现有的 `.icon-button` 等样式。

## Form Focus Styling

- 默认禁止为 `input` 和 `textarea` 添加外侧高亮，包括 `outline`、`box-shadow` 或发光效果；聚焦态优先只调整边框颜色或背景色。
- 只有明确的无障碍需求或错误态需要外侧提示时，才能局部覆盖该约束，并且必须使用语义明确、范围受控的样式。

## Directory Structure

前端按“应用编排、业务功能、共享基础设施、样式资源”分层。新增文件应放在拥有它的业务边界内，不要继续把页面逻辑堆回 `App.tsx` 或把领域组件平铺到一个公共目录。

```text
src/
├── app/
│   ├── App.tsx                 # 应用入口，组合工作区与错误边界
│   └── error-boundary.css      # 应用错误回退页面样式
├── features/
│   ├── workspace/              # 工作区外壳、跨领域状态编排和工作区偏好
│   │   ├── ProjectWorkspace.tsx
│   │   ├── WorkspaceHeader.tsx
│   │   ├── useProjectWorkspace.ts
│   │   ├── useWorkspaceData.ts
│   │   └── sessionPreference.ts
│   ├── projects/               # Project -> Session 导航和 Project 创建流程
│   │   ├── ProjectsPanel.tsx
│   │   ├── NewProjectDialog.tsx
│   │   ├── projectGroups.ts
│   │   ├── navigation.css
│   │   ├── styles.css
│   │   ├── theme.css
│   │   └── types.ts
│   ├── sessions/               # Session 页面外壳和空状态
│   │   ├── SessionPanel.tsx
│   │   ├── styles.css
│   │   └── useSessionCommands.ts
│   ├── agents/                 # Agent 对话、任务输入、列表和运行状态
│   │   ├── AgentsPanel.tsx
│   │   ├── AgentConversation.tsx
│   │   ├── AgentSessionPanel.tsx
│   │   ├── AgentTaskInput.tsx
│   │   ├── PrimaryAgentTaskInput.tsx
│   │   ├── useAgentCommands.ts
│   │   ├── useAgentData.ts      # 查询、选择、分页、事件订阅及过期请求保护
│   │   ├── agentStore.ts        # 按 Agent ID 保存唯一快照，分别通知查询和实时输出订阅
│   │   ├── history.ts           # 历史记录身份与跨页回填
│   │   ├── useAgentTaskInput.ts # 草稿、模型和 reasoning 选择
│   │   ├── streaming.ts         # 流式归并与持久化去重纯函数
│   │   ├── composer.css
│   │   ├── conversation.css    # 对话滚动容器
│   │   ├── styles.css
│   │   ├── theme.css
│   │   ├── types.ts            # Agent 状态及乐观消息类型
│   │   └── output/             # 消息、Markdown、工具及协作结果呈现
│   │       ├── MessageViews.tsx
│   │       ├── conversation.css
│   │       └── styles.css
│   ├── timing/                 # 操作耗时查询、事件合并和展示
│   └── settings/               # Settings 总览与模型配置
│       ├── SettingsPanel.tsx
│       ├── styles.css
│       ├── settings.css
│       └── model-config/
│           ├── ModelConfigPanel.tsx
│           ├── ProviderDirectory.tsx
│           ├── ProviderDialog.tsx
│           ├── ModelDialog.tsx
│           ├── ModelConfigDialogParts.tsx
│           ├── modelConfigHelpers.ts
│           ├── modelConfigTypes.ts
│           ├── dialogs.css
│           └── styles.css
├── api/                        # Wails binding、DTO 和 API 调用边界
│   ├── bindings.ts             # Wails binding 发现和可用性检查
│   ├── timings.ts              # 操作耗时 DTO、查询及校验
│   ├── projects.ts             # Project DTO 和调用
│   ├── sessions.ts             # Session DTO 和调用
│   ├── agents.ts               # Agent DTO、调用和事件订阅
│   ├── commands.ts             # Agent 输入和控制命令
│   ├── models.ts               # 模型目录和配置调用
│   ├── errors.ts               # API 错误码
│   └── index.ts                # API 公共导出入口
├── shared/
│   └── errors.ts               # 跨 feature 共享的 API 错误文案
├── components/
│   └── ui/                     # 与业务无关、可跨 feature 复用的 UI 原子组件
│       ├── Overlay.tsx         # L4 modal Portal 与关闭交互
│       └── index.ts
├── styles/
│   ├── global.css              # 全局 token、reset、字体和基础标题样式
│   ├── common.css              # 跨功能的错误提示、空状态及通用控件
│   ├── workspace.css           # Workspace 样式入口，按顺序引入模块
│   └── workspace/              # 按职责拆分的 Workspace 样式
│       ├── base.css            # 工作区基础、顶部栏、通用控件和状态
│       ├── layout.css          # Shell / Scroll / Page 容器契约
│       ├── navigation.css      # 工作区导航壳层和 Agent 侧栏基础样式
│       ├── dialogs.css         # 通用弹窗基础样式
│       ├── responsive.css      # 基础响应式及动效规则
│       ├── theme-shell.css     # Shell 和顶部栏的颜色、边框与字体
│       ├── theme-navigation.css # 导航区域的视觉状态，不拥有几何布局
│       ├── theme-dialogs.css   # 当前主题的弹窗和错误状态
│       └── theme-responsive.css # 减少动效时的视觉过渡
└── main.tsx                    # React/Wails 前端入口
```

分层约束：

- `app/` 可以组合 `features/workspace`，但 `features` 不应反向依赖 `app/`。
- `features/workspace/` 负责组合各业务 feature，并维护跨领域的工作区状态；它不承载具体项目、Session、Agent 或设置页面的展示规则。
- `agents/useAgentTaskInput.ts` 只管理任务草稿和执行选项；命令由 `useAgentCommands.ts` 执行。`streaming.ts` 只做不可变的事件归并与持久化去重，不订阅事件。
- Agent 查询快照由 `agentStore.ts` 按 ID 唯一保存，选中的 Agent 和历史从快照派生；请求代次留在查询 Hook。命令通过 `beginInput` / `acceptInput` / `rejectInput` 等业务动作更新乐观消息，不接收内部集合的 setter。
- 高频输出只由 Agent 对话区域订阅；不得让文本 delta 触发整个工作区、项目导航和 Agent 侧栏的状态更新。
- 历史从最新页向前加载，游标使用稳定记录身份；刷新最新页时保留已加载旧页并补齐中间跨度，不能用固定的最早 N 条代替完整历史。
- Agent 状态类型由 `agents/types.ts` 拥有，`agents/output/` 消费状态并展示内容；`timing/` 独立管理耗时数据，不反向依赖 `agents/`；模型请求计时与 token 的组合展示由 `agents/output/MessageTiming.tsx` 拥有。
- 每个业务 feature 内部就近放置组件、hooks、types 和专属样式；只有确实跨领域复用的内容才放入 `shared/`，Wails 调用统一放入 `api/`。
- `projects/`、`sessions/`、`agents/` 和 `settings/` 之间不直接互相组合；跨领域流程由 `workspace/` 编排。
- `api/` 是前端与 Go runtime 的唯一数据边界，按 Wails binding 和领域拆分；`index.ts` 是其公共导出入口。
- `shared/` 不包含具体页面布局、业务状态或 Wails binding。
- `components/ui/` 只放通用视觉组件，不承载 Project workspace、Session 或 Agent 的业务规则。
- 全局样式从 `main.tsx` 引入，Workspace 样式只通过 `app/App.tsx` 的 `workspace.css` 入口加载，应用错误回退样式由错误边界组件引入。公共布局按 `base -> layout -> navigation` 排列，公共控件和功能样式随后加载；会话与消息的完整样式位于 Shell 主题之后，通用弹窗和响应式覆盖保持明确顺序。
- 对话滚动容器由 `agents/conversation.css` 拥有，消息内容由 `agents/output/conversation.css` 拥有；新增样式不得借主题文件重复声明已有布局。合并样式须保留状态选择器、断点及减少动效规则，并核对计算样式。
- 工作区容器使用 `app-shell`（L0 Shell）、布局 Region（L1）、`scroll-region`（L2）和 `page`（L3）。L1 不滚动，L2 是区域内唯一滚动容器，L3 不设置 `height`、`overflow` 或 `transform`。
- 设置和模型配置页在单一 Scroll 内组合 sticky 头部与 Page，短窗口取消 sticky；通用工作区断点集中于 `workspace/responsive.css`，在功能样式之后加载。
- 所有 modal 使用 `components/ui/Overlay.tsx` 并挂载到 `#overlay-root`。锚定触发器的菜单保留在组件内并使用全局 z-index token。

## 验证

- `npm run lint` 检查 Rules of Hooks 和完整依赖；`npm run check` 依次执行 lint 和生产构建。
- 修改 Wails binding 签名后，在 `backend/` 执行 `go run -tags bindings .` 重新生成绑定，再执行前端检查；不手工编辑生成的 `wailsjs/` 文件。
