# Model Registry

> 本文定义 `backend/internal/modelregistry` 的目标目录、模型配置所有权、credential 边界、解析语义和并发规则。
> 模型协议适配器见 [`model_provider.md`](model_provider.md)，AgentRuntime 的模型请求边界见 [`application/agent_runtime/model_request.md`](application/agent_runtime/model_request.md)，总目录见 [`directory-structure.md`](directory-structure.md)。

## 1. 角色

`internal/modelregistry` 是进程内模型配置注册表。它加载并校验用户配置的 Provider、模型、能力、profile binding 和 credential 状态，并通过稳定查询把模型选择解析为不可变的运行配置。

`modelregistry` 管理“配置了哪些模型以及如何选择它们”，`modelprovider` 管理“如何调用某种远程模型协议”。两者不能合并：

```text
ModelSelection
    -> modelregistry resolve
    -> ResolvedModelConfig
    -> compose selects protocol adapter
    -> modelprovider ModelStream
```

`modelregistry` 负责：

- 加载、校验和原子保存模型配置；
- 维护 Provider、模型和 profile 的进程内索引；
- 校验 ProviderID、ModelID、APIFormat、上下文窗口和输出上限；
- 规范化 reasoning capability 和默认档位；
- 独立加载和保存 Provider credential；
- 返回不包含明文 credential 的设置页投影；
- 将 profile 或显式模型引用解析为固定 `ModelSelection`；
- 将 `ModelSelection` 解析为创建 adapter 所需的不可变配置；
- 在配置替换时提供一致的并发视图。

`modelregistry` 不负责：

- 编码 Anthropic/OpenAI 请求；
- 发起 Provider HTTP 请求或解析 SSE；
- import 具体 `modelprovider` 协议包；
- 构造 `ModelRequest` 或消费模型输出；
- 控制 execution、工具调用、transcript 或 settlement；
- 将明文 API key 暴露给 binding DTO、projection 或日志。

## 2. 目录结构

```text
backend/internal/modelregistry/
├── doc.go                         包职责、并发和敏感信息边界
├── types.go                       配置、APIFormat、profile 和公开投影类型
├── registry.go                    Registry、内存索引和只读快照
├── load.go                        models 与 secrets 文件加载
├── validate.go                    完整配置校验与规范化
├── update.go                      配置替换和原子持久化
├── credentials.go                 credential set、clear 和内部解析
├── catalog.go                     已配置模型目录与 profile 展开
├── resolve.go                     ModelSelection 和 adapter 配置解析
├── load_test.go                   文件格式、权限和损坏输入测试
├── validate_test.go               配置不变量测试
├── credentials_test.go            credential 隔离与脱敏测试
└── resolve_test.go                profile、模型和 reasoning 解析测试
```

该目录使用单一 Go package：

```go
package modelregistry
```

文件按职责拆分，但共享同一个 `Registry` 和锁。不得为 config、credential 或 catalog 再创建只含一个文件的无意义子包。

## 3. 配置模型

配置至少包含以下结构：

```text
FileConfig
├── Providers[] ProviderConfig
├── Models[] ModelConfig
└── Profiles map[AgentProfile] ModelReference

ProviderConfig
├── ID
├── DisplayName
├── BaseURL
└── ProxyURL?

ModelConfig
├── ProviderID
├── ModelID
├── Label
├── APIFormat
├── ContextWindow
├── MaxOutputTokens
└── ReasoningConfig
```

`ProviderConfig` 不包含 API key。模型配置文件可以进入普通配置备份；credential 必须保存在独立 secrets 文件中。

`APIFormat` 是协议选择，不是厂商名称：

```text
anthropic_messages
openai_chat_completions
openai_responses
```

一个 Provider 可以配置多个模型，不同模型可以选择不同 API format。`ProviderID + ModelID` 构成 registry 内唯一模型键。

## 4. 文件职责

### 4.1 `types.go`

定义：

- `ProviderConfig`、`ModelConfig` 和 `FileConfig`；
- `ModelAPIFormat`；
- `ReasoningConfig`；
- `ModelReference` 和 profile binding；
- 不含敏感信息的 `ModelOption`、`ProviderOption`；
- 仅供组合根创建 adapter 使用的 `ResolvedModelConfig`。

类型必须使用明确字段，不使用 `map[string]any` 表达稳定配置。

### 4.2 `registry.go`

定义 `Registry`、构造函数、锁和内存索引：

```text
Registry
├── config snapshot
├── providersByID
├── modelsByKey
├── profileBindings
├── credential store
└── revision
```

所有读取返回防御性副本。调用方不能取得 registry 内部 slice、map、raw JSON buffer 或 credential map 的可变引用。

### 4.3 `load.go`

负责读取 models 和 secrets 文件、限制文件大小、执行严格 JSON 解码并拒绝尾随内容。文件不存在时只能按明确的首次启动规则建立默认值，损坏文件不能静默覆盖。

### 4.4 `validate.go`

负责完整配置校验和规范化：

- ID 格式与唯一性；
- Provider 和模型引用完整性；
- BaseURL、ProxyURL 和 APIFormat；
- `0 < MaxOutputTokens < ContextWindow`；
- reasoning levels 去重、默认值和 supported 约束；
- profile allowlist 及其模型引用；
- 输入对象不被原地修改。

### 4.5 `update.go`

负责配置替换。更新顺序为：

```text
validate defensive copy
    -> build replacement indexes
    -> write temporary file
    -> fsync and atomic rename
    -> lock registry
    -> swap snapshot and indexes
    -> increment revision
```

持久化失败时内存配置不得变化；内存替换后所有新查询必须看到同一个 revision 的完整快照。

### 4.6 `credentials.go`

负责 credential 的独立存储和最小读取边界：

- `SetProviderCredential`；
- `ClearProviderCredential`；
- `HasProviderCredential`；
- 仅供 `compose` 创建 adapter 使用的内部 credential resolve；
- secrets 文件的 `0600` 权限、临时文件、fsync 和原子替换。

面向 app 和前端的查询只能返回 `hasCredential`。不提供会进入 binding 的 `GetAPIKey` 或包含明文 key 的配置快照。

### 4.7 `catalog.go`

负责基于当前 registry snapshot 生成已配置模型目录，包括 label、Provider 展示名、reasoning capability 和默认 profile。它不调用远程 `/models` endpoint，也不解析 Provider 返回值。

远程模型发现属于外部模型协议能力，由独立端口和 `modelprovider` adapter 实现。发现结果只有经过显式配置更新和完整校验后才能进入 registry，不能直接修改当前索引。

### 4.8 `resolve.go`

提供两种解析：

```text
AgentProfile
    -> ModelSelection

ModelSelection
    -> ResolvedModelConfig
```

`ModelSelection` 只包含 execution 需要冻结的 ProviderID、ModelID 和 reasoning。`ResolvedModelConfig` 供 `compose` 创建具体 adapter，至少包含：

```text
ResolvedModelConfig
├── ProviderID
├── ModelID
├── APIFormat
├── BaseURL
├── ProxyURL?
├── ContextWindow
├── MaxOutputTokens
├── Reasoning
└── RegistryRevision
```

明文 credential 不放入 `ResolvedModelConfig`。`compose` 通过单独的窄 credential source 取得创建 adapter 所需的 key，创建完成后不再向其他层传递。

## 5. Profile 与 Execution 快照

profile binding 是模型选择默认值，不是 execution 运行期动态引用：

```text
AgentProfile
    -> registry resolves ModelSelection
    -> execution start freezes ModelSelection
    -> AgentRuntime uses frozen selection
```

配置或 profile 在 execution 运行期间发生变化，不得改变 active execution 的 Provider、模型或 reasoning。后续 execution 才使用新的 registry revision。

`ContextWindow` 和 `MaxOutputTokens` 等模型能力由 resolver 在 activation 时读取并形成运行配置。已经 durable 创建的 execution 必须保留足以审计其模型选择的 ID 和配置 revision。

## 6. Credential 边界

models 配置和 secrets 使用两个独立文件：

```text
models.json             Provider、模型、能力和 profile
model-secrets.json      ProviderID -> credential
```

credential 必须满足：

- 不进入 models config、SQLite、JSONL transcript 或审计事件；
- 不进入 `ModelSelection`、`ResolvedModelConfig`、projection 或 binding DTO；
- 不出现在 URL、错误、日志和测试快照；
- set/clear command 与普通模型配置更新分离；
- 保存时使用限制权限、fsync 和原子 rename；
- Provider 删除时明确处理孤立 credential，不能通过普通配置响应返回其内容。

## 7. 并发与一致性

`Registry` 是进程内单一配置所有者。读取可以并发，配置和 credential 更新必须串行。

配置更新采用 copy-validate-swap：调用方传入的数据先深拷贝和校验，再构建完整索引，最后在锁内一次替换。禁止在共享 map 上逐项修改，使读者看到一半旧配置、一半新配置。

credential 更新和 config 更新使用独立的 durable 文件，但删除 Provider、清理 credential 等跨文件动作必须定义失败收敛方式。任何失败都不能把旧配置文件截断，也不能把明文 credential 写入补偿日志。

## 8. 与 Model Provider 的装配

`modelregistry` 和 `modelprovider` 互不 import。组合发生在 `compose`：

```text
application ModelResolver
    -> compose providerModelResolver
        -> modelregistry.ResolveModel(selection)
        -> modelregistry.ResolveCredential(providerID)
        -> select adapter by APIFormat
        -> modelprovider/<protocol>.New(config)
        -> core/runtime.ModelStream
```

`compose` 中的选择必须穷举所有 `ModelAPIFormat`。未知 format 返回稳定配置错误，不能默认回退到某个兼容协议。

registry 返回配置事实，protocol adapter 负责远程调用；任何一方都不读取另一方的内部存储或具体类型。

## 9. 依赖规则

```text
modelregistry
    -> standard library
    -> internal/core/domain/security
    -> internal/core/runtime 的稳定配置类型

compose
    -> modelregistry
    -> modelprovider protocol packages
```

`modelregistry` 不 import `app`、`contracts`、`application`、`orchestration`、`storage`、`modelprovider`、`tools` 或 `compose`。具体文件系统访问由该配置适配器自身封装，不泄漏 `os.File` 给内层。

## 10. 验证要求

至少覆盖：

- 重复 ProviderID 和重复模型键；
- 未知 Provider、APIFormat、profile 和 reasoning level；
- context window 与 max output tokens 边界；
- models/secrets 文件不存在、损坏、超限和包含尾随 JSON；
- 原子保存中途失败时保留旧文件和旧内存快照；
- 并发读取与配置替换；
- 输入和返回值的深拷贝；
- credential set、clear、权限和脱敏；
- profile 解析和 registry revision；
- binding/projection 序列化结果不包含明文 credential。

## 11. 不变量

1. `modelregistry` 管理模型配置和选择，不实现任何远程 Provider 协议。
2. `ProviderID + ModelID` 在一个 registry revision 内唯一。
3. 配置更新先 durable 保存，再原子替换完整内存快照。
4. 所有读取返回防御性副本，不暴露共享可变状态。
5. credential 与普通模型配置分离，公开查询只返回是否已配置。
6. `ModelSelection` 一旦进入 execution snapshot，不随 registry 更新而变化。
7. `ResolvedModelConfig` 不包含明文 credential 或具体 adapter 类型。
8. `modelregistry` 与 `modelprovider` 只通过 `compose` 组合，彼此不 import。
9. 未知 API format 必须失败，不能静默选择兼容 adapter。
10. registry 不发起模型请求、不消费模型流，也不推进 execution 状态。
