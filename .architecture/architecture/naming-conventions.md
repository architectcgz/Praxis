# Praxis 命名规范

> 状态：规范性目标架构。本文定义 Go、TypeScript、Wails contract、领域对象、应用用例、只读投影、前端组件和文件的统一命名规则。

## 1. 基本原则

命名必须优先表达业务语义、所属边界和生命周期，不使用技术层无法解释的泛化后缀。

1. 同一个名称在不同层表达相同概念，不因传输或存储方式改变业务含义。
2. 不同边界的类型使用不同名称，禁止跨层复用 DTO、应用参数、领域对象或持久化记录。
3. 后缀只在能够表达稳定语义时使用。
4. 集合、状态、身份和时间语义必须从名称中可直接判断。
5. 缩写在同一种语言内保持统一大小写。
6. 名称不携带临时实现细节、UI 框架名称或存储技术名称，除非该类型只属于对应 adapter。
7. `Model` 是 AI 模型领域术语，可以出现在类型名中；不能把 `Model` 当作任意数据结构的通用后缀。

## 2. 分层命名

| 边界 | 输入 | 输出 | 状态类型 | 示例 |
|---|---|---|---|---|
| Wails / 外部 contract | `*Request` | `*Response` | `*Event`、`*Status` | `CreateProjectRequest`、`CreateProjectResponse` |
| Application use case | `*Params` | `*Result` | 业务语义名 | `SendInputParams`、`SendInputResult` |
| Domain | 值对象或命令语义名 | 实体、值对象 | `*Status`、`*Outcome` | `AgentExecution`、`ExecutionStatus` |
| Core projection | 查询参数按方法参数表达 | `*Summary`、`*Details`、`*Snapshot` | `*Snapshot` | `SessionSummary`、`ContextSnapshot` |
| Storage adapter | adapter 私有参数 | `*Row`、`*Record` | 持久化格式私有类型 | `executionRow`、`receiptRecord` |
| Provider wire adapter | 协议私有 request | 协议私有 response/event | 协议原生语义 | `anthropicRequest`、`responseEvent` |
| Frontend API | `*Request` | `*Response` | `*Event` | `SaveModelProviderConfigRequest` |
| Frontend feature | 业务动作参数 | 业务对象 | `*State` | `ModelProviderConfig`、`SessionState` |
| Frontend UI | Props | React component | 局部状态不导出 | `SettingsPage`、`ModelPicker` |

边界转换必须显式发生：

```text
contracts.SendInputRequest
    -> start.SendInputParams
    -> execution.ExecutionInputSnapshot
    -> contracts.SendInputResponse
    -> frontend api SendInputResponse
```

## 3. Contract 命名

`internal/contracts` 和 `frontend/src/api` 描述 Wails 传输边界。

### 3.1 Request

`Request` 只表示从外部调用方进入后端的传输输入：

```text
CreateProjectRequest
SaveModelProviderConfigRequest
ControlAgentRequest
```

规则如下：

- 名称由明确动词和业务对象组成；
- 同一请求的顶层类型使用 `Request` 后缀；
- 嵌套值对象不重复添加 `Request`，使用其实际语义名；
- Request 不进入 application 或 domain；
- 查询没有参数时不创建空 Request 类型；
- 敏感写入字段必须使用动作语义，例如 `CredentialChange`，不能伪装成可读取状态。

### 3.2 Response

`Response` 只表示后端返回前端的顶层传输结果：

```text
CreateProjectResponse
GetModelProviderConfigResponse
SaveModelProviderConfigResponse
```

规则如下：

- 顶层返回类型使用与 binding 方法对应的 `Response`；
- 嵌套类型按内容命名，例如 `CredentialStatus`、`ModelOption`、`ProjectSummary`；
- Response 不作为领域对象或 registry 内部配置；
- 公开响应必须已经完成脱敏和错误映射；
- 无返回数据的命令直接返回 error，不创建 `EmptyResponse`、`SuccessResponse` 或 `BaseResponse`。

### 3.3 Event

主动推送到前端的消息使用 `Event`：

```text
AgentOutputEvent
ExecutionSettledEvent
ModelCatalogChangedEvent
```

`Event` 表示可丢失的通知或流事件，不代替权威查询结果。持久化领域事件使用具体领域名称，并由所属模块定义，不能复用前端 Event DTO。

## 4. Application 命名

Application use case 使用 `Params` 和 `Result`：

```go
type CreateProjectParams struct { /* ... */ }
type CreateProjectResult struct { /* ... */ }

func (s *Service) CreateProject(ctx context.Context, params CreateProjectParams) (CreateProjectResult, error)
```

规则如下：

- `Params` 表示一个用例的完整输入；
- `Result` 表示用例成功后的业务结果；
- application 不使用 `Request`、`Response` 或 `DTO`；
- 没有结构化输出时只返回 `error`；
- 一个 package 内只有一个明确 owner 时可以命名为 `Service`；
- application service 不重复拼接所属 package 名称，跨包使用 package identifier 提供业务上下文，例如 `toolinvocation.Service`；
- application service 的实现类型不使用 `IService` 或其他接口式后缀；
- 接口按能力或行为命名，例如 `ToolInvoker`、`ToolExecutor`、`ModelResolver`、`ExecutionActivator`，不使用没有额外语义的 `Service` 后缀；
- 外层聚合多个领域能力时，可以使用 `ProjectService`、`SessionService` 等名称区分依赖，但这不改变具体 application package 内使用 `Service` 的规则；
- 跨模块协调器使用具体职责名，例如 `ExecutionScheduler`、`DeliveryCoordinator`；
- 不使用 `Manager`、`Handler`、`Processor` 或 `Engine` 代替缺失的职责名称。

## 5. Domain 命名

领域类型直接使用业务术语：

```text
Project
Session
Agent
AgentExecution
CapabilityGrant
WaitCondition
```

规则如下：

- 领域实体不添加 `Entity`、`DomainModel` 或 `Data` 后缀；
- 值对象使用其业务含义，例如 `ModelSelection`、`ContextSelection`；
- 生命周期枚举使用 `Status`，终止结果使用 `Outcome`，稳定失败分类使用 `FailureCode`；
- 领域行为使用业务动词，例如 `Start`、`Pause`、`Settle`、`Approve`；
- 领域对象不使用 transport 的 `Request`、`Response`、`Option` 或 UI 组件后缀；
- 业务中真实存在的请求对象可以使用 `Request`，例如持久化的 `AgentControlRequest`，其注释必须明确它不是传输 DTO。

## 6. 查询与只读类型

只读类型按数据完整度和时间语义命名。

### 6.1 Summary

`Summary` 是列表和概览使用的紧凑只读数据：

```text
ProjectSummary
SessionSummary
AgentSummary
```

它只包含识别、排序和概览所需字段，不承诺完整对象数据。

### 6.2 Details

`Details` 是一个对象的完整只读详情：

```text
ProjectDetails
SessionDetails
ExecutionDetails
```

使用复数形式 `Details`。不使用 `DetailData`、`Info` 或 `FullModel`。

### 6.3 Snapshot

`Snapshot` 表示绑定到明确时刻、revision 或执行边界的不可变副本：

```text
ContextSnapshot
ExecutionSecuritySnapshot
RuntimeExecutionSnapshot
HealthSnapshot
```

只有满足以下条件时才使用 `Snapshot`：

- 数据对应明确时间点或 revision；
- 创建后不会随权威状态变化；
- 调用方获得防御性副本；
- 名称或字段能够识别其一致性边界。

普通查询响应不能仅因为是复制出来的数据就命名为 `Snapshot`。

### 6.4 Option

`Option` 表示可供用户或调用方选择的紧凑候选项：

```text
ModelOption
ReasoningOption
ProviderOption
```

`Option` 必须包含稳定选择值和必要展示字段。不可选择的详情对象、配置对象或任意列表元素不能使用 `Option`。

### 6.5 Projection

`Projection` 表示后端只读模型的职责或生成过程，主要用于 package、service 或架构描述。具体返回类型继续使用 `Summary`、`Details`、`Snapshot`、`Option` 或业务名。

前端类型和组件不使用 `Projection` 后缀。

## 7. Config、Settings 与 Document

### 7.1 Config

`Config` 表示构造、运行或持久化所需的明确配置：

```text
ModelProviderConfig
ProviderConfig
ExecutionEngineConfig
```

同一聚合的完整配置使用聚合名称加 `Config`，不使用 `Document` 表示“整个 JSON 文件”。

### 7.2 Settings

`Settings` 表示用户偏好，通常允许缺省并具有默认值：

```text
WorkspaceSettings
EditorSettings
RetrySettings
```

协议端点、credential、模型能力和领域策略属于 `Config` 或具体领域类型，不因可编辑而统一称为 Settings。

### 7.3 Document

`Document` 只用于产品中真实存在、具有独立文档身份或文档内容语义的对象：

```text
PolicyDocument
MarkdownDocument
```

JSON 配置根、API 响应、表单状态和多个实体的组合结果不能仅因“整体传输或保存”而使用 `Document`。

## 8. 前端命名

### 8.1 数据类型

前端 API 层保留 transport 语义：

```text
GetModelProviderConfigResponse
SaveModelProviderConfigRequest
AgentOutputEvent
```

API 层完成校验和规范化后，feature 层使用业务语义名：

```text
ModelProviderConfig
GroupedModelCatalog
SessionSummary
CredentialStatus
```

前端数据类型禁止使用以下泛化后缀：

```text
*View
*ViewModel
*Data
*Info
*DTO
*Payload
```

`Payload` 仅可用于确实没有更具体语义的第三方协议原始载荷，并且只能存在于 adapter 内部。

### 8.2 React 组件

React 组件按交互职责命名：

| 后缀 | 使用场景 | 示例 |
|---|---|---|
| `Page` | 路由或主内容页面 | `SettingsPage` |
| `Panel` | 工作区中的稳定区域 | `SessionPanel` |
| `Dialog` | 模态交互 | `NewSessionDialog` |
| `Picker` | 选择一个值 | `ModelPicker` |
| `Menu` | 一组命令或导航项 | `AgentActionsMenu` |
| `List` | 重复项集合 | `ProviderList` |
| `Item` / `Row` | 集合中的单项 | `ProviderItem` |
| `Form` | 独立提交表单 | `ProviderForm` |
| `Editor` | 可持续编辑复杂内容 | `PolicyEditor` |
| `EmptyState` | 明确的空状态 | `SessionEmptyState` |
| `ErrorBoundary` | React 错误边界 | `WorkspaceErrorBoundary` |

前端组件和文件禁止使用 `*View` 作为“展示某些内容”的通用后缀。标准技术名词 `WebView` 不受此规则影响。

组件 Props 使用 `<ComponentName>Props`。事件回调使用 `on<Action>`，内部处理函数使用 `handle<Action>`：

```text
ModelPickerProps
onModelChange
handleModelChange
```

Hook 使用 `use<Capability>`，例如 `useProjectWorkspace`、`useModelCatalog`。

## 9. 方法与函数

方法名称使用动词开头，并与副作用语义一致。

| 前缀 | 语义 |
|---|---|
| `Get` | 按唯一身份读取一个结果；不存在时返回明确错误或空值契约 |
| `List` | 返回零到多个结果 |
| `Find` | 尝试查找，未找到是正常结果 |
| `Resolve` | 根据引用和规则得到确定结果，失败表示引用或配置无效 |
| `Create` | 创建具有新稳定身份的对象 |
| `Save` | 保存一个完整聚合或配置 |
| `Update` | 修改已有对象的一部分 |
| `Delete` | 删除权威对象 |
| `Set` | 设置单个明确值 |
| `Clear` | 清除可选值 |
| `Validate` | 只校验，不持久化或执行外部副作用 |
| `Normalize` | 返回规范化副本，不执行外部副作用 |
| `Build` | 从已有输入构建新值，不读取隐式全局状态 |
| `Discover` | 从外部系统获取候选项，不直接写入权威配置 |

查询方法不能产生隐藏写入。命令方法不能使用 `Get`、`List` 或 `Find` 掩盖副作用。

## 10. Identity、集合与布尔值

### 10.1 Identity

稳定身份统一使用 `<Concept>ID`：

```text
ProjectID
SessionID
AgentID
ExecutionID
ProviderID
ModelID
GroupID
```

Go 使用 `ID`，TypeScript 和 JSON 使用 `Id`：

```text
Go:         ProviderID
TypeScript: providerId
JSON:       providerId
```

显示名称使用 `DisplayName` / `displayName`。只有领域中唯一明确的主名称才使用 `Name`。

### 10.2 集合

集合使用复数名词：

```text
providers
models
executionIds
reasoningLevels
```

Map 名称表达 key 与 value 的关系：

```text
providersByID
modelsByKey
profileBindings
```

禁止使用 `list`、`map`、`array` 作为唯一业务信息，例如 `modelList`、`providerMap`。当容器类型本身已清楚时直接使用复数业务名。

### 10.3 布尔值

布尔字段优先使用可直接判断真假的名称：

```text
isActive
hasCredential
canRetry
shouldRefresh
reasoningSupported
```

避免双重否定和含义不明确的状态：

```text
notDisabled
noError
flag
statusOK
```

命令式 Props 可以使用平台惯例 `disabled`、`required`、`readOnly`。

## 11. 缩写与大小写

Go 导出标识符中的常用缩写保持全大写：

```text
ID
URL
API
HTTP
JSON
SQL
DTO
```

示例：

```text
ProviderID
BaseURL
APIKey
HTTPClient
JSONValue
```

TypeScript、JSON 和 CSS 将缩写视为普通单词：

```text
providerId
baseUrl
apiKey
httpClient
jsonValue
```

类型名仍使用 PascalCase：

```text
ApiError
HttpClient
JsonValue
```

公开 JSON 字段统一使用 lower camel case。禁止在同一 contract 中混用 `baseURL`、`baseUrl`、`providerID` 和 `providerId`。

## 12. 文件与目录

### 12.1 Go

- package identifier 使用简短、全小写、无下划线名称；
- 多词架构目录可以使用下划线，package identifier 仍保持合法简洁；
- 文件使用 snake_case，例如 `send_input.go`、`model_request.go`；
- 测试文件使用 `<subject>_test.go`；
- `service.go` 只承载共享接收者、依赖和构造函数；
- 用例、查询和 adapter 按具体职责命名文件。

### 12.2 TypeScript / React

- React 组件文件使用 PascalCase，并与默认或主要导出组件同名；
- Hook 文件使用 `useXxx.ts`；
- 普通模块使用 lower camel case，例如 `sessionPreference.ts`；
- API 模块使用复数业务名，例如 `models.ts`、`sessions.ts`；
- CSS class 使用 kebab case，并包含稳定的组件或功能语义；
- 文件名不能使用 `View`、`Data`、`Info` 代替具体职责。

## 13. 禁止的泛化命名

以下名称不能作为缺少领域语义时的默认选择：

```text
Data
Info
ItemData
Object
Base
Common
Generic
Helper
Helpers
Utils
Manager
Handler
Processor
Wrapper
View
ViewModel
ModelData
ConfigDocument
```

确实属于基础设施标准概念时可以使用对应术语，例如 HTTP `Handler`、React `ErrorBoundary`、序列化 `Document`。使用者必须能够从所属 package 和完整类型名判断其唯一职责。

## 14. 审查清单

新增或修改名称时必须确认：

1. 名称是否表达业务概念而不是数据形状。
2. 类型所属层是否可以从后缀判断。
3. 是否错误复用了 Request、Response、Params、Result 或 Snapshot。
4. 前端是否使用了 `View`、`ViewModel`、`Data`、`Info` 或 `DTO` 泛化后缀。
5. `Document` 是否确实表示具有文档语义的对象。
6. ID、URL、API 等缩写是否符合当前语言规则。
7. 布尔值、集合和 map 是否能从名称判断含义。
8. 组件后缀是否准确表达页面、面板、弹窗、选择器或列表职责。
9. 方法动词是否与读取、写入、解析或发现语义一致。
10. 同一概念在 Go、TypeScript 和 JSON 中是否保持一致。

## 15. 不变量

1. 前端类型、组件和文件不使用泛化的 `*View` 或 `*ViewModel` 命名。
2. 外部传输使用 `Request`、`Response` 和 `Event`，application use case 使用 `Params` 和 `Result`。
3. 领域对象使用业务名称，不添加 `Entity`、`DTO` 或 `Data` 后缀。
4. `Snapshot` 必须具有明确的时间、revision 或执行一致性边界。
5. `Option` 只表示可选择候选项。
6. `Document` 只表示真实文档，不表示配置根或 API envelope。
7. Go 使用 `ID`、`URL`、`API`，TypeScript 与 JSON 使用 `Id`、`Url`、`Api`。
8. 同一类型不能跨 contract、application、domain 和 storage 边界复用。
9. 泛化容器名和职责不明的技术后缀不能代替业务命名。
10. 所有公开名称必须能从名称本身判断概念、边界或职责。
