import { useState, type ReactNode } from 'react'
import { BrainCircuit, ChevronDown, ChevronRight, Cpu, Pencil, Plus, Server } from 'lucide-react'
import type { ModelConfigOption, ProviderConfigOption } from '../../../api'
import { formatAPIFormat, modelKey, providerDisplayName } from './modelConfigHelpers'

type ProviderDirectoryListProps = {
    providers: ProviderConfigOption[]
    models: ModelConfigOption[]
    onEditProvider: (index: number) => void
    onEditModel: (index: number, providerID: string) => void
    onAddModel: (providerID: string) => void
}

export function ProviderDirectoryList({
    providers,
    models,
    onEditProvider,
    onEditModel,
    onAddModel,
}: ProviderDirectoryListProps) {
    const [expandedProviders, setExpandedProviders] = useState<Set<string>>(
        () => new Set(providers.map((provider) => provider.id)),
    )

    const toggleProvider = (providerId: string) => {
        setExpandedProviders((current) => {
            const next = new Set(current)
            if (next.has(providerId)) {
                next.delete(providerId)
            } else {
                next.add(providerId)
            }
            return next
        })
    }

    if (!providers.length) {
        return (
            <EmptyConfiguration
                icon={<Server size={22} />}
                title="没有连接的 Provider"
                detail="添加 Provider，然后配置您想使用的 Model。"
            />
        )
    }

    return (
        <div className="provider-directory-list">
            {providers.map((provider, providerIndex) => {
                const providerModels = models.reduce<Array<{ model: ModelConfigOption; index: number }>>(
                    (result, model, modelIndex) => {
                        if (model.providerId === provider.id) {
                            result.push({ model, index: modelIndex })
                        }
                        return result
                    },
                    [],
                )
                const isExpanded = expandedProviders.has(provider.id)

                return (
                    <section className="provider-directory provider-card" key={provider.id}>
                        <header className="provider-directory-header">
                            <button
                                className="provider-collapse-toggle"
                                type="button"
                                aria-expanded={isExpanded}
                                aria-label={`${isExpanded ? '折叠' : '展开'} ${providerDisplayName(provider)}`}
                                onClick={() => toggleProvider(provider.id)}
                            >
                                {isExpanded ? (
                                    <ChevronDown size={18} aria-hidden="true" />
                                ) : (
                                    <ChevronRight size={18} aria-hidden="true" />
                                )}
                            </button>
                            <span className="provider-directory-icon" aria-hidden="true">
                                <Server size={18} />
                            </span>
                            <div className="provider-directory-main">
                                <div className="provider-directory-title">
                                    <h2>{providerDisplayName(provider)}</h2>
                                    <span className="model-config-tag">
                                        {providerModels.length} 个 Model
                                    </span>
                                </div>
                                <p>
                                    <code>{provider.baseURL}</code>
                                </p>
                            </div>
                            <button
                                className="icon-button provider-directory-edit"
                                type="button"
                                title={`编辑 ${providerDisplayName(provider)}`}
                                aria-label={`编辑 ${providerDisplayName(provider)}`}
                                onClick={() => onEditProvider(providerIndex)}
                            >
                                <Pencil size={15} aria-hidden="true" />
                            </button>
                        </header>
                        {isExpanded && (
                            <section
                                className="provider-model-directory"
                                aria-label={`${providerDisplayName(provider)} 的 Model`}
                            >
                                <h3>Model</h3>
                                {providerModels.length ? (
                                    <div className="provider-model-list">
                                        {providerModels.map(({ model, index }) => (
                                            <ProviderModelItem
                                                key={modelKey(model.providerId, model.modelId)}
                                                model={model}
                                                onEdit={() => onEditModel(index, provider.id)}
                                            />
                                        ))}
                                    </div>
                                ) : (
                                    <p className="provider-model-empty">
                                        此 Provider 没有配置 Model。
                                    </p>
                                )}
                                <button
                                    className="provider-add-model"
                                    type="button"
                                    onClick={() => onAddModel(provider.id)}
                                >
                                    <Plus size={15} aria-hidden="true" />
                                    <span>添加 Model</span>
                                </button>
                            </section>
                        )}
                    </section>
                )
            })}
        </div>
    )
}

function ProviderModelItem({
    model,
    onEdit,
}: {
    model: ModelConfigOption
    onEdit: () => void
}) {
    return (
        <article className="provider-model-item">
            <span className="provider-model-icon" aria-hidden="true">
                <Cpu size={18} />
            </span>
            <div className="provider-model-main">
                <div className="provider-model-title">
                    <h4>{model.label || model.modelId}</h4>
                </div>
                <p>
                    <code>{model.modelId}</code>
                </p>
                <div className="model-config-tags">
                    <span className="model-config-tag">
                        {formatAPIFormat(model.apiFormat)}
                    </span>
                    <span className="model-config-tag">
                        {model.contextWindow.toLocaleString()} 上下文
                    </span>
                    <span className="model-config-tag">
                        {model.maxOutputTokens.toLocaleString()} 最大输出
                    </span>
                    {model.reasoningLevels.length > 0 && (
                        <span className="model-config-tag is-blue">
                            <BrainCircuit size={11} />
                            {model.reasoningLevels.length} 个推理级别
                        </span>
                    )}
                </div>
            </div>
            <button
                className="icon-button provider-model-edit"
                type="button"
                title={`编辑 ${model.label || model.modelId}`}
                aria-label={`编辑 ${model.label || model.modelId}`}
                onClick={onEdit}
            >
                <Pencil size={15} aria-hidden="true" />
            </button>
        </article>
    )
}

function EmptyConfiguration({
    icon,
    title,
    detail,
}: {
    icon: ReactNode
    title: string
    detail: string
}) {
    return (
        <div className="provider-directory-empty">
            {icon}
            <div>
                <strong>{title}</strong>
                <p>{detail}</p>
            </div>
        </div>
    )
}
