# Frontend Guidelines

## Style Guide

- 前端视觉风格的唯一规范入口是 [`STYLE.md`](STYLE.md)。开始任何前端任务前必须先读取它。
- 新增或修改组件、页面、交互状态和样式时必须遵守 `STYLE.md`；稳定视觉决策发生变化时，在同一次变更中更新该文档。

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
│   └── App.tsx                 # 页面级状态、Wails 命令编排和布局组合
├── features/
│   └── projects/               # Project workspace 及其 Session / Agent 层级
│       ├── ProjectsPanel.tsx   # Project -> Session 导航树
│       ├── SessionPanel.tsx    # 当前 Session 内容与交互
│       ├── AgentsPanel.tsx     # 当前 Session 下的 Agent 列表
│       └── types.ts             # Project / Session / Agent 视图类型
├── shared/
│   └── api.ts                  # Wails binding、DTO 和 API 错误定义
├── components/ui/              # 与业务无关、可跨 feature 复用的 UI 原子组件
├── styles/
│   ├── global.css              # 全局 reset、字体和页面基础样式
│   ├── workspace.css           # Workspace 样式入口，按顺序引入模块
│   └── workspace/              # 按职责拆分的 Workspace 样式
│       ├── base.css            # 工作区基础、顶部栏、通用控件和状态
│       ├── navigation.css      # Project / Session / Agent 导航面板
│       ├── content.css         # Session 内容、消息线程和输入区
│       ├── dialogs.css         # 新建 Project / Session 弹窗
│       └── responsive.css      # 响应式断点和 reduced-motion 覆盖
├── assets/                     # 字体、图片等静态资源
└── main.tsx                    # React/Wails 前端入口
```

分层约束：

- `app/` 可以组合所有 `features`，但 `features` 不应反向依赖 `app/`。
- `features/<name>/` 内部优先就近放置组件、hooks、types 和该领域 API；只有确实跨领域复用的内容才放入 `shared/`。
- `shared/` 不包含具体页面布局或业务状态；Wails binding 是前端与 Go runtime 的唯一数据边界。
- `components/ui/` 只放通用视觉组件，不承载 Project workspace、Session 或 Agent 的业务规则。
- 全局样式从 `main.tsx` 引入，Workspace 样式由 `app/App.tsx` 引入；新增样式应归入对应层级。Workspace 模块只能通过 `workspace.css` 入口加载，且保持 `base -> navigation -> content -> dialogs -> responsive` 的顺序。
