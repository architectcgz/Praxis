# Model Registry

> 本文定义 `backend/internal/modelregistry` 的 Provider 配置聚合、Group 语义、credential 边界、模型目录投影和运行时解析规则。协议 adapter 的生命周期见 [`model_provider/provider.md`](../model_provider/provider.md)，请求映射见 [`model_provider/request.md`](../model_provider/request.md)，流解析见 [`model_provider/stream.md`](../model_provider/stream.md)，HTTP 安全约束见 [`model_provider/http.md`](../model_provider/http.md)。

`modelregistry` 是 Model Provider 配置的唯一 owner。它管理配置文档、不可变索引和运行时解析，不编码 Provider 请求、不发起模型调用，也不解析远程流。

## 源码目录

```text
backend/internal/modelregistry/
├── doc.go                         包职责、并发和敏感信息边界
├── types.go                       配置聚合、API format 和公开投影类型
├── registry.go                    Registry、不可变快照和只读索引
├── load.go                        owner-only 配置文件加载
├── validate.go                    聚合校验与规范化
├── update.go                      revision 检查和原子配置替换
├── credentials.go                 Provider credential mutation 与脱敏
├── catalog.go                     Group 化模型目录投影
├── resolve.go                     Profile 与敏感运行配置解析
├── discovery.go                   Provider 远程模型发现
└── *_test.go                      配置、并发、脱敏和解析 contract test
```

该目录使用单一 `modelregistry` package。文件按职责拆分，但共享同一个 `Registry`、revision 和不可变 snapshot；不得把 Group、Provider、Model 或 credential 拆成平行事实源。

## 1. 聚合模型

Model Provider 配置以 Provider 为聚合根。Provider 同时拥有访问端点、credential、默认 API format 和可用 Model，不存在与 Provider 平行维护的全局 Model 或 API key 配置。

Group 是用户定义的模型组织维度。Model 通过 `GroupID` 归入一个 Group；Group 不参与远程路由，也不进入 execution 的模型身份。

```text
ModelProviderConfig
├── Revision
├── Groups[] GroupConfig
├── Providers[] ProviderConfig
│   ├── Credential?
│   └── Models[] ModelConfig
└── Profiles map[AgentProfile] ModelReference

GroupConfig
├── ID
└── DisplayName

ProviderConfig
├── ID
├── DisplayName
├── BaseURL
├── ProxyURL?
├── Credential?
├── DefaultAPIFormat
└── Models[] ModelConfig

ModelConfig
├── ID
├── DisplayName
├── GroupID
├── APIFormatOverride?
├── ContextWindow
├── MaxOutputTokens
└── Reasoning
```

类型之间遵守以下所有权规则：

1. Provider 是 endpoint、credential 和 Models 的唯一所有者。
2. Model 只存在于一个 Provider 内，不能作为顶层独立配置重复保存。
3. 每个 Model 必须属于且只属于一个 Group。
4. 一个 Group 可以包含来自多个 Provider 的 Model。
5. 一个 Provider 的不同 Model 可以属于不同 Group。
6. `ProviderID + ModelID` 是运行时唯一模型键。
7. `GroupID` 只影响配置管理和模型选择目录，不改变模型键。
8. Profile 直接引用 `ProviderID + ModelID`，不引用 Group。

## 2. Group

`GroupConfig` 是用户拥有的有序分类。典型 Group 可以是 `GPT`、`Claude`、`Gemini`、`Local` 或用户定义的任务类别。

```go
type GroupConfig struct {
    ID          string
    DisplayName string
}
```

Group 规则如下：

- `ID` 是稳定标识，创建后不因展示名修改而变化；
- `ID` 在整个文档内唯一，并符合配置 ID 格式；
- `DisplayName` 去除首尾空白后必须非空，名称按大小写折叠后唯一；
- `Groups` 数组顺序是用户定义的目录顺序；
- Group 可以暂时不包含 Model，并继续出现在配置管理界面；
- 模型选择目录省略没有可选择 Model 的空 Group；
- 删除仍被 Model 引用的 Group 必须校验失败；
- Model 在 Group 之间移动只修改 `GroupID`，不修改 `ProviderID`、`ModelID`、Profile 引用或 execution 快照。

Group 不包含 endpoint、credential、API format、reasoning 或模型能力。任何远程调用配置都必须归 Provider 或 Model 所有。

## 3. Provider

`ProviderConfig` 表示一条可独立认证和调用的模型访问通道。相同服务的不同账号、endpoint、proxy 或认证信息使用不同 `ProviderID`。

```go
type ProviderConfig struct {
    ID               string
    DisplayName      string
    BaseURL           string
    ProxyURL          string
    Credential       *CredentialRecord
    DefaultAPIFormat ModelAPIFormat
    Models            []ModelConfig
}
```

Provider 规则如下：

- `ID` 全局唯一，是 credential、Profile 和运行时解析使用的稳定身份；
- `DisplayName` 是用户可编辑名称，不承担引用语义；
- `BaseURL` 和 `ProxyURL` 经过结构化 URL 校验与规范化；
- `DefaultAPIFormat` 定义该 Provider 下 Model 的默认协议；
- `Models` 的数组顺序是该 Provider 配置中的稳定顺序；
- credential 嵌套在 Provider 内，并随 Provider 一起创建、更新和删除；
- Provider 删除时，其 Models、credential 和运行时索引在同一次配置提交中删除；
- 存在 Profile 引用时，提交方必须同时提供新的有效绑定或删除对应绑定，不能产生悬空引用。

Provider 的展示名、Group 名称或 Model 展示名均不能用于选择协议。有效 API format 只能来自已校验的 `DefaultAPIFormat` 或 Model override。

## 4. Model

`ModelConfig` 表示远程 Provider 下一个可选择的 Model。

```go
type ModelConfig struct {
    ID                string
    DisplayName       string
    GroupID           string
    APIFormatOverride *ModelAPIFormat
    ContextWindow     int
    MaxOutputTokens   int
    Reasoning         ReasoningConfig
}
```

Model 的有效 API format 按以下规则计算：

```text
EffectiveAPIFormat = APIFormatOverride ?? Provider.DefaultAPIFormat
```

Model 规则如下：

- `ID` 是发送给远程 API 的模型标识；
- `ID` 在所属 Provider 内唯一，不要求跨 Provider 唯一；
- `DisplayName` 为空时规范化为 `ID`；
- `GroupID` 必须引用当前文档中的 Group；
- `APIFormatOverride` 只用于同一 Provider 下确实采用不同协议的 Model；
- `ContextWindow` 必须大于零；
- `MaxOutputTokens` 必须大于零且小于 `ContextWindow`；
- reasoning 的 supported、levels 和 default 必须形成完整有效配置；
- Model 的任何编辑都不能隐式修改其他 Provider 下同名 Model。

运行时和 durable execution 只保存：

```text
ModelSelection
├── ProviderID
├── ModelID
└── Reasoning
```

Group、展示名、credential 和 endpoint 不进入 `ModelSelection`。Execution 需要审计配置版本时保存 `RegistryRevision`。

## 5. Credential

Credential 是 Provider 聚合内的敏感值对象，不是独立业务资源。

```go
type CredentialRecord struct {
    Type CredentialType
    Key  string
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"
```

目标配置只提供 Provider 级 credential 操作，不提供脱离 Provider 的 key map、key 列表或公开读取命令。

持久化记录可以包含明文 key；管理查询和前端 DTO 只能获得脱敏状态：

```go
type CredentialStatus struct {
    Type       CredentialType
    Configured bool
}
```

配置写入使用显式 mutation，避免编辑 Provider 元数据时覆盖现有 key：

```go
type CredentialChange struct {
    Operation CredentialOperation // keep | replace | clear
    Key       string              // Used only by replace.
}
```

约束如下：

- `keep` 保留该 Provider 当前 credential；
- `replace` 要求非空 key，并用新值替换当前 credential；
- `clear` 删除该 Provider credential；
- 新 Provider 不能使用 `keep` 继承其他 Provider 的 credential；
- Provider 删除会删除其 credential；
- credential key 不通过任何 query、event、error、log、metric 或 test snapshot 返回；
- credential 不进入 `ModelRequest`、`ModelStreamEvent`、SQLite、transcript 或 execution snapshot；
- 只有 registry 内部 runtime resolver 可以取得包含明文 credential 的运行配置。

## 6. 持久化文档

完整配置保存在一个 owner-only 文件中，默认路径为：

```text
~/.praxis/config/models.json
```

该文件由 `DataRoot.ModelProvidersConfig` 定位，权限为 `0600`。Group、Provider、credential、Model 和 Profile 作为一个 revision 原子持久化。

```json
{
  "revision": 7,
  "groups": [
    { "id": "gpt", "displayName": "GPT" },
    { "id": "claude", "displayName": "Claude" }
  ],
  "providers": [
    {
      "id": "gateway-codex",
      "displayName": "Codex Gateway",
      "baseUrl": "https://gateway.example.com",
      "proxyUrl": "http://127.0.0.1:7897",
      "credential": {
        "type": "api_key",
        "key": "<secret>"
      },
      "defaultApiFormat": "openai_responses",
      "models": [
        {
          "id": "gpt-5",
          "displayName": "GPT-5",
          "groupId": "gpt",
          "contextWindow": 128000,
          "maxOutputTokens": 8192,
          "reasoning": {
            "supported": true,
            "levels": ["low", "medium", "high"],
            "default": "medium"
          }
        }
      ]
    },
    {
      "id": "gateway-claude",
      "displayName": "Claude Gateway",
      "baseUrl": "https://gateway.example.com",
      "credential": {
        "type": "api_key",
        "key": "<secret>"
      },
      "defaultApiFormat": "anthropic_messages",
      "models": [
        {
          "id": "claude-opus",
          "displayName": "Claude Opus",
          "groupId": "claude",
          "contextWindow": 200000,
          "maxOutputTokens": 8192,
          "reasoning": {
            "supported": true,
            "levels": ["low", "medium", "high"],
            "default": "medium"
          }
        }
      ]
    }
  ],
  "profiles": {
    "primary": { "providerId": "gateway-codex", "modelId": "gpt-5" }
  }
}
```

持久化必须使用严格 JSON 解码、文件大小上限、临时文件、`fsync` 和同目录原子 rename。损坏、超限或包含未知字段的文档不能进入内存 registry，也不能被默认配置覆盖。

## 7. 配置命令

配置管理使用一个 revision 化文档接口：

```text
GetModelProviderConfig()
    -> GetModelProviderConfigResponse

SaveModelProviderConfig(
    ExpectedRevision,
    Groups,
    Providers with CredentialChange,
    Profiles
)
    -> SavedRevision | ValidationError | RevisionConflict
```

`GetModelProviderConfigResponse` 保留 Provider 聚合结构，但把 `CredentialRecord` 投影为 `CredentialStatus`。调用方看不到 key，也不需要通过第二个资源拼接 Provider 与 credential 状态。

保存顺序固定为：

```text
serialize writers
    -> compare ExpectedRevision
    -> copy current snapshot
    -> apply credential mutations to matching Providers
    -> normalize and validate complete aggregate
    -> build replacement indexes and catalog
    -> write owner-only temporary file
    -> fsync and atomic rename
    -> swap in-memory snapshot
    -> publish new revision
```

任何步骤失败时，当前 durable 文件、内存 snapshot 和 revision 保持不变。并发编辑使用 `ExpectedRevision` 检测冲突，不能以最后写入覆盖其他用户已经保存的 Provider、Group、Model 或 credential 更新。

## 8. Registry 索引

Registry 从同一个不可变 snapshot 构建以下索引：

```text
groupsByID       GroupID -> GroupConfig
groupOrder       GroupID -> ordinal
providersByID    ProviderID -> ProviderConfig
modelsByKey      (ProviderID, ModelID) -> ModelConfig
profiles         AgentProfile -> ModelReference
catalog          grouped ModelOption[]
```

索引不成为第二事实源。每次配置提交都从完整文档重新构建并整体替换。

读取规则如下：

- 所有公开读取返回防御性副本；
- 一个读取操作只能观察到一个 revision；
- registry 内部不得向调用方暴露可变 slice、map 或 credential 指针；
- Group 顺序来自 `Groups` 数组；
- Provider 和 Model 配置顺序来自各自数组；
- 按 Group 聚合目录时，同组内按 Provider 顺序和 Provider 内 Model 顺序保持稳定；
- Profile 解析和显式模型解析必须使用同一个 snapshot。

## 9. 模型目录投影

模型选择目录以 Group 为第一层，以 Model route 为选择项：

```text
GroupedModelCatalog
└── Groups[] GroupOption
    └── Models[] ModelOption
```

```go
type GroupOption struct {
    ID          string
    DisplayName string
    Models      []ModelOption
}

type ModelOption struct {
    GroupID         string
    ProviderID      string
    ProviderName    string
    ModelID         string
    DisplayName     string
    Available       bool
    Reasoning       ReasoningOption
    DefaultProfiles []string
}
```

目录规则如下：

- 前端模型选择器按 Group 展示，不渲染全局平铺 Model 列表；
- Model 项同时展示 Model 名称和 Provider 名称，以区分同一模型的不同访问通道；
- `Available` 至少由 credential 是否配置、Provider 配置是否有效和协议是否受支持决定；
- 不可用 Model 保留在配置管理投影中；选择目录可以展示为禁用项，但不能作为新 execution 的有效选择；
- 空 Group 不进入选择目录；
- 当前选中 Model 所属 Group 由 `GroupID` 直接定位；
- Group 改名或 Model 换组后，现有 `ProviderID + ModelID` 选择仍然有效。

设置管理投影保持 Provider 为第一层，以便在同一聚合内编辑 endpoint、credential、默认协议和 Models。选择目录保持 Group 为第一层，以便用户按自己定义的分类查找 Model。两个投影都由同一个 registry snapshot 生成。

## 10. 运行时解析

运行时解析只接收 `ModelSelection`，并一次性得到完整 Provider/Model route：

```text
ModelSelection(ProviderID, ModelID, Reasoning)
    -> Registry.ResolveRuntimeModel
    -> ResolvedRuntimeModel
    -> compose selects protocol adapter
    -> modelprovider/<protocol>.New
    -> runtime.ModelStream
```

```go
type ResolvedRuntimeModel struct {
    ProviderID       string
    ModelID          string
    APIFormat        ModelAPIFormat
    BaseURL          string
    ProxyURL         string
    APIKey           string
    ContextWindow    int
    MaxOutputTokens  int
    Reasoning        string
    RegistryRevision uint64
}
```

`ResolvedRuntimeModel` 是只允许 registry 与 compose 使用的敏感内部类型。它不实现 JSON 序列化，不进入 application DTO，也不提供给 binding、projection 或前端。

解析规则如下：

1. 使用复合键查找 Provider 和 Model；
2. 校验 Provider credential 已配置；
3. 计算有效 API format；
4. 校验 reasoning level；
5. 复制 endpoint、proxy、credential 和模型能力到一次性运行配置；
6. 记录当前 registry revision；
7. 由 compose 穷举选择具体协议 adapter。

Group 不参与以上步骤。活动 execution 使用已经冻结的 `ModelSelection` 和解析 revision，配置更新只影响之后创建的 model stream。

## 11. 远程模型发现

远程模型发现以 Provider 为边界：

```text
DiscoverProviderModels(ProviderID)
    -> resolve Provider aggregate and credential
    -> call Provider model catalog endpoint
    -> return discovered model IDs
```

发现结果是临时候选，不直接写入 `Provider.Models`，也不自动分配 Group。用户确认的 Model 必须携带有效 `GroupID`，通过完整配置命令保存后才能进入 registry 和模型选择目录。

远程发现不能返回 credential、完整 Provider 配置或原始错误 body。并发配置更新导致 Provider revision 变化时，保存仍以 `ExpectedRevision` 判定是否接受发现结果。

## 12. 边界与依赖

目标职责分布如下：

```text
app bindings
    -> sanitized configuration commands and grouped catalog queries

modelregistry
    -> document load/save
    -> Group/Provider/Model validation
    -> credential ownership and redaction
    -> immutable indexes
    -> profile and runtime resolution

compose
    -> consume ResolvedRuntimeModel
    -> select concrete protocol adapter

modelprovider/*
    -> encode request
    -> perform HTTP/SSE call
    -> normalize stream events
```

`modelprovider` 协议包不读取配置文件，不查询 Group，也不管理 credential 生命周期。`modelregistry` 不编码协议请求，不解析 SSE，也不 import 具体协议包。两者只在 `compose` 中组合。

前端只依赖脱敏配置 DTO 和 grouped catalog DTO。前端状态中不得保存、缓存或回显已持久化 key；API key 输入在提交完成后立即清空。

## 13. 验证要求

配置与投影至少覆盖以下 contract test：

- Group ID、名称、顺序和重复校验；
- Model 引用未知 Group；
- 一个 Provider 下重复 Model ID；
- 不同 Provider 使用相同 Model ID；
- Provider 默认 API format 和 Model override 解析；
- Profile 复合引用完整性；
- credential 的 keep、replace、clear 和 Provider 删除；
- 配置 query、catalog、错误和日志不包含 key；
- revision conflict 不写文件、不替换 snapshot；
- 原子保存失败保留当前 revision；
- Group 改名和 Model 换组不改变模型复合键；
- grouped catalog 的 Group、Provider 和 Model 顺序稳定；
- 并发读取只能看到完整的单一 revision；
- remote discovery 不修改 registry；
- runtime resolve 才能取得 credential，公开 DTO 无法取得。

## 14. 不变量

1. Provider 是 endpoint、credential、默认 API format 和 Models 的单一聚合根。
2. Credential 只能作为 Provider 的敏感子对象存在。
3. 每个 Model 必须属于一个用户定义 Group；Group 不参与远程路由。
4. 一个 Group 可以聚合多个 Provider 的 Model，一个 Provider 也可以向多个 Group 提供 Model。
5. `ProviderID + ModelID` 是配置、Profile、execution 和运行时解析共享的唯一模型身份。
6. 配置文件、内存 snapshot、索引和 grouped catalog 在任意时刻对应同一个 revision。
7. 配置保存与 credential 更新是一次原子聚合提交。
8. 明文 credential 只存在于 owner-only 持久化文件、registry 敏感内存和 adapter 认证 header。
9. Binding、前端、日志、错误、事件、transcript 和 execution snapshot 永远不包含明文 credential。
10. 配置管理使用 Provider 视角，模型选择使用 Group 视角，两者由同一个事实源生成。
11. Group 改动不能改变活动 execution 已冻结的模型选择。
12. 未知 Group、Provider、Model、API format 或 reasoning level 必须显式失败。
