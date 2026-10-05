# Frontend Guidelines

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
│   │   ├── useAgentData.ts      # 查询、缓存、选择、事件订阅及过期请求保护
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
- Agent 状态类型由 `agents/types.ts` 拥有，`agents/output/` 消费状态并展示内容；`timing/` 独立管理耗时数据。
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
