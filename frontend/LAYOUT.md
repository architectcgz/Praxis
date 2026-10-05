# 页面布局与容器设计指导

本文定义 Praxis 前端页面的容器职责、模块边界、滚动、裁剪和层叠规则。新增或调整页面结构、滚动区、侧栏、工具栏、菜单和浮层时，按本文划分 CSS 所有权。

## 容器分层

页面按 `Shell → Region → Scroll → Page → Module` 组织。每一层只拥有自己的布局职责，不把滚动、裁剪和定位职责随意传给后代。

| 层级 | 职责 | 约束 |
|---|---|---|
| Shell | 占满应用视口，限制应用整体尺寸 | 可使用 `overflow: hidden` 限定视口；不用于承载局部滚动 |
| Region | 划分侧栏、主内容和辅助面板 | 使用 Grid/Flex 分配空间；自身不滚动 |
| Scroll | 承载一个区域内需要滚动的内容 | 每个区域明确唯一滚动所有者 |
| Page | 控制内容最大宽度和页面排版 | 不设置固定高度、`overflow` 或 `transform` |
| Module | 组织局部组件和控件 | 只管理模块内部布局，不改变页面级容器职责 |

典型结构：

```text
.app-shell
└── .workspace-grid
    ├── .projects-panel
    │   └── .scroll-region
    ├── .main-panel
    │   └── .scroll-region
    │       └── .page
    └── .agents-panel
        └── .scroll-region
```

```css
.app-shell {
    display: grid;
    grid-template-rows: 42px minmax(0, 1fr);
    height: 100dvh;
    overflow: hidden;
}

.workspace-grid {
    display: grid;
    grid-template-columns: 248px minmax(0, 1fr) 268px;
    min-width: 0;
    min-height: 0;
}

.region {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
}

.scroll-region {
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
    overflow: auto;
}

.page {
    width: min(100%, var(--page-width, 920px));
    min-width: 0;
    margin-inline: auto;
}
```

`minmax(0, 1fr)`、`min-width: 0` 和 `min-height: 0` 用于允许 Grid/Flex 子项收缩到可用空间内。不要用额外的 `overflow: hidden` 掩盖子项无法收缩的问题。

## 尺寸与滚动所有权

- Shell 拥有视口高度，Region 拥有分栏和面板尺寸，Scroll 拥有滚动行为，Page 拥有内容宽度。
- 一个视觉区域只指定一个主要滚动容器。滚动容器的头部、工具栏和底部操作应作为其兄弟节点保留在可视区域。
- 设置与模型配置页采用 `Region → Scroll → sticky 头部 + Page`：头部和内容共享阅读宽度与滚动槽；窗口高度不超过 `560px` 时头部取消 sticky，避免挤占全部可用空间。
- 对话页面使用消息线程作为滚动区，输入区作为固定的 Flex/Grid 兄弟区域；不要让整个会话面板和消息线程同时滚动。
- 代码、日志等局部内容确实需要独立滚动时，可在组件内部设置有明确最大尺寸的局部滚动区，并避免形成多层滚轮嵌套。
- Region 和 Page 使用 `min-width: 0`、`min-height: 0` 控制内容收缩；长文本、代码和表格由内容组件处理换行、省略或局部滚动。
- 加载态占用父 Region 的剩余空间，空状态由 Scroll 承载，不用 `vh` 或固定百分比高度占位。Page 不添加会留下 stacking context 的入场动画。

## `overflow` 与裁剪

`overflow` 同时影响滚动和后代绘制范围。设置前先确定它是在建立滚动区，还是有意裁剪内容。

| 设置 | 用途 | 注意事项 |
|---|---|---|
| `visible` | 默认允许内容绘制到盒子外 | 适用于需要显示阴影或锚定菜单的容器 |
| `auto` / `scroll` | 建立明确的滚动区 | 只放在指定的 Scroll 层或有限尺寸的局部内容区 |
| `hidden` | 限制视口、裁切媒体或截断内容 | 会裁掉后代菜单和浮层，不能当通用布局修复 |
| `clip` | 只裁切绘制范围，不提供滚动 | 同样会裁掉越界菜单和浮层 |

- App Shell 可以裁切到应用窗口边界；Region 和滚动容器不应无理由增加裁剪。
- 弹出内容若被截断，沿 DOM 树向上检查所有 `overflow` 祖先。提高 `z-index` 不能解除裁剪。
- 若浮层设计为跨 Region 显示，必须确认从浮层到目标边界之间没有裁剪祖先，并让所属 Region 在同级 Region 中具有正确层级。
- 若浮层不需要覆盖其他 Region，应调整它的对齐和展开方向，使其留在所属区域内。

## 层叠上下文与层级

所有层级值集中定义在 `src/styles/global.css`，组件只使用已有 token：

| Token | 用途 |
|---|---|
| `--z-base` | 普通内容 |
| `--z-raised` | 需要高于普通内容的局部控件或区域 |
| `--z-popover` | 菜单、选择器等非模态浮层 |
| `--z-sticky` | 固定或粘性的页面导航 |
| `--z-modal` | 模态对话框 |

- 全局 token 的数值及语义顺序由 `global.css` 统一维护。若某类浮层需要覆盖另一类页面层级，调整全局层级关系，不在组件中写裸数字或随意增大局部值。
- `z-index` 只能比较处于同一 stacking context 下的层级。子元素不能越过其 stacking context 父级，与外部兄弟元素直接比较。
- `position` 配合非 `auto` 的 `z-index`、`transform`、`filter`、`backdrop-filter`、小于 `1` 的 `opacity`、`isolation` 和 `contain` 等属性可能创建 stacking context。
- 只在需要定义同级覆盖关系的容器上建立层级。普通模块不应为了“保险”设置 `z-index`、`transform` 或 `isolation`。
- `box-shadow` 只表达视觉深度，不改变绘制顺序，也不能解决遮挡。

## 菜单、浮层与模态框

- 锚定触发器的菜单默认留在所属组件内：锚点使用 `position: relative`，菜单使用 `position: absolute` 和 `--z-popover`。
- 锚定菜单应考虑可用空间，选择向上/向下展开及左/右对齐，避免越过所属 Region 边界。
- 只有明确要求跨 Region 覆盖时，才提升所属 Region 的层级；同时检查所有祖先的裁剪和 stacking context。
- 不为单个菜单创建任意 Portal 或独立层级体系。模态框统一使用 `components/ui/Overlay.tsx` 并挂载到 `#overlay-root`。
- 模态导航抽屉也使用 `Overlay`；嵌套弹窗仅最上层可交互并处理 `Escape`、Tab，关闭后恢复入口焦点，最后一层关闭后释放滚动锁。
- 打开菜单时要验证点击菜单项、点击外部关闭、`Escape` 关闭和键盘焦点行为；视觉层级不能破坏交互命中区域。

## 样式模块边界

- `src/styles/global.css` 拥有全局 token、reset 和基础元素规则。
- `src/styles/workspace/` 拥有 Shell、Region、通用滚动容器和工作区响应式布局。
- `src/features/<feature>/` 拥有该功能的组件布局和局部样式；跨功能复用的样式才进入公共样式目录。
- 布局文件拥有尺寸、Grid/Flex、定位和滚动职责；主题覆盖文件主要拥有颜色、边框、阴影和材质，不重复声明同一布局属性。
- 一个布局属性应有清晰的主要所有者。确需覆盖时，按 `workspace.css` 的加载顺序放置，并限定选择器和适用状态。
- 业务类名使用 feature 语义前缀，避免 `.panel`、`.content` 等泛化类名跨模块互相覆盖。

## 响应式布局

- 弹性列使用 `minmax(0, 1fr)`；固定侧栏只在空间允许时保留，窄屏通过断点收起或切换导航方式。
- 空间不足时优先重排、隐藏次要面板或改变浮层对齐，不缩小文字到难以阅读的尺寸。
- 每个断点都要检查 Region 收起后的最小宽度、滚动区高度和浮层是否仍在视口内。
- 响应式规则集中在对应布局或响应式文件中，不在多个 feature 中重复定义相同的工作区断点。
- 顶部栏为 `42px`；工作区超过 `1180px` 时为 `248px / 弹性主区 / 268px` 三栏，`1180px` 及以下隐藏 Agent 侧栏，`860px` 及以下隐藏项目侧栏。更窄的断点只重排组件，不把 Grid 切换为依赖 `height: 100%` 的 Block 布局。
- `860px` 及以下在顶部栏显示项目导航按钮；通过左侧抽屉复用同一个 `ProjectsPanel`，搜索、项目/会话选择、新建和设置入口保持可达。抽屉宽度不超过 `320px`，保留至少 `44px` 遮罩区域；标题和底部固定，中间列表独立滚动，背景工作区使用 `inert` 隔离交互。
- 选择导航目标后关闭抽屉，关闭按钮、遮罩和 `Escape` 也可关闭。窗口变宽时恢复桌面侧栏；有会话编辑或确认弹窗时等待弹窗关闭，避免尺寸变化丢弃草稿。`ProjectWorkspace` 的媒体查询与 `workspace/responsive.css` 的 `860px` 断点保持一致。
- 通用工作区断点由 `workspace/responsive.css` 拥有，并在功能样式之后加载；主题文件只保留视觉状态和视觉过渡。

## 排查顺序

发生遮挡、裁切或滚动异常时，按以下顺序检查：

1. 检查元素和祖先的盒子边界，以及 Grid/Flex 子项是否设置了 `min-width: 0`、`min-height: 0`。
2. 检查从目标元素到预期绘制边界之间的 `overflow`；先确认是否被裁切。
3. 检查目标与遮挡元素最近的 stacking context，以及哪个同级容器拥有层级。
4. 检查是否存在多个滚动容器或布局属性在主题文件中被重复覆盖。
5. 最后才调整全局层级 token 或菜单定位。

布局变更后，至少检查完整桌面布局、侧栏收起断点、窄屏、各滚动区域、菜单展开和模态框覆盖关系。

## 构建验证

```bash
npm --prefix frontend run build
```
