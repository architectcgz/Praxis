import { useCallback, useEffect, useState } from 'react'
import { AlertCircle, ArrowLeft, LoaderCircle, Plus } from 'lucide-react'
import {
    getModelConfig,
    saveModelConfig,
    setProviderKey,
    type ModelConfigDocument,
    type ModelConfigOption,
    type ProviderConfigOption,
} from '../../../api'
import { readableError } from '../../../shared/errors'
import { ProviderDialog } from './ProviderDialog'
import { ProviderDirectoryList } from './ProviderDirectory'
import { ModelDialog } from './ModelDialog'
import { Field } from './ModelConfigDialogParts'
import { providerDisplayName, modelKey } from './modelConfigHelpers'
import type { Editor, ModelConfigPanelProps } from './modelConfigTypes'

export function ModelConfigPanel({ onRefresh, onBack }: ModelConfigPanelProps) {
    const [config, setConfig] = useState<ModelConfigDocument | null>(null)
    const [editor, setEditor] = useState<Editor | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')

    const load = useCallback(async () => {
        setLoading(true)
        setError('')
        try {
            setConfig(await getModelConfig())
        } catch (reason) {
            setError(readableError(reason))
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        void load()
    }, [load])

    const saveConfig = async (next: ModelConfigDocument) => {
        const result = await saveModelConfig(next)
        if (!result.saved) {
            throw new Error(result.validationError || '无法保存 Model 配置。')
        }
        return getModelConfig()
    }

    const persist = async (next: ModelConfigDocument) => {
        setSaving(true)
        setError('')
        try {
            setConfig(await saveConfig(next))
            onRefresh()
            setEditor(null)
        } catch (reason) {
            setError(readableError(reason))
        } finally {
            setSaving(false)
        }
    }

    const saveProvider = async (provider: ProviderConfigOption, key: string): Promise<void> => {
        if (!config || editor?.kind !== 'provider') {
            return
        }
        const providers = [...config.providers]
        if (editor.index === null) {
            providers.push(provider)
        } else {
            providers[editor.index] = provider
        }
        const defaultProviderId = config.defaultProviderId || (config.providers.length === 0 ? provider.id : '')
        setSaving(true)
        setError('')
        try {
            const savedConfig = await saveConfig({ ...config, defaultProviderId, providers })
            setConfig(savedConfig)
            if (key.trim()) {
                await setProviderKey(provider.id, key.trim())
            }
            const refreshedConfig = key.trim() ? await getModelConfig() : savedConfig
            setConfig(refreshedConfig)
            onRefresh()
            setEditor(null)
        } catch (reason) {
            setError(readableError(reason))
        } finally {
            setSaving(false)
        }
    }

    const saveDefaultProvider = async (defaultProviderId: string) => {
        if (!config || !defaultProviderId || defaultProviderId === config.defaultProviderId) {
            return
        }
        await persist({ ...config, defaultProviderId })
    }

    const saveModel = async (model: ModelConfigOption) => {
        if (!config || editor?.kind !== 'model') {
            return
        }
        const nextModel = { ...model, providerId: editor.providerID }
        if (config.models.some((item, index) => (
            modelKey(item.providerId, item.modelId) === modelKey(nextModel.providerId, nextModel.modelId) && index !== editor.index
        ))) {
            setError(`Model ID "${nextModel.modelId}" 已为此 Provider 配置。`)
            return
        }
        const models = [...config.models]
        const previousModel = editor.index === null ? null : config.models[editor.index]
        if (editor.index === null) {
            models.push(nextModel)
        } else {
            models[editor.index] = nextModel
        }
        const providers = config.providers.map((provider) => {
            if (provider.id !== editor.providerID) {
                return provider
            }
            if (!provider.defaultModelId || previousModel?.modelId === provider.defaultModelId) {
                return { ...provider, defaultModelId: nextModel.modelId }
            }
            return provider
        })
        await persist({ ...config, models, providers })
    }

    const deleteProvider = async (index: number) => {
        if (!config) {
            return
        }
        const provider = config.providers[index]
        const providers = config.providers.filter((_, itemIndex) => itemIndex !== index)
        await persist({
            ...config,
            defaultProviderId: config.defaultProviderId === provider.id ? providers[0]?.id || '' : config.defaultProviderId,
            providers,
            models: config.models.filter((model) => model.providerId !== provider.id),
        })
    }

    const deleteModel = async (index: number) => {
        if (!config) {
            return
        }
        const model = config.models[index]
        const remainingModels = config.models.filter((_, itemIndex) => itemIndex !== index)
        await persist({
            ...config,
            models: remainingModels,
            providers: config.providers.map((provider) => (
                provider.id === model.providerId && provider.defaultModelId === model.modelId
                    ? {
                        ...provider,
                        defaultModelId: remainingModels.find((item) => item.providerId === provider.id)?.modelId || '',
                    }
                    : provider
            )),
        })
    }

    const modelEditorProvider = editor?.kind === 'model'
        ? config?.providers.find((provider) => provider.id === editor.providerID)
        : undefined

    return (
        <section className="region model-config-region" aria-labelledby="model-config-title">
            <div className="scroll-region">
                <div className="page page-header model-config-header">
                    <header className="model-config-heading">
                        <button className="back-link" type="button" onClick={onBack}>
                            <ArrowLeft size={16} aria-hidden="true" /> <span>设置</span>
                        </button>
                        <div className="model-config-header-content">
                            <div className="model-config-header-text">
                                <span className="empty-kicker">Provider 配置</span>
                                <h1 id="model-config-title">Provider 与 Model</h1>
                                <p>首先添加 Provider，然后配置 Praxis 将在该 Provider 下使用的 Model。</p>
                            </div>
                            <div className="model-config-header-actions">
                                <div className="model-config-default-provider">
                                    <Field label="默认 Provider">
                                        <select
                                            value={config?.defaultProviderId || ''}
                                            disabled={loading || saving || !config?.providers.length}
                                            onChange={(event) => void saveDefaultProvider(event.target.value)}
                                        >
                                            {!config?.providers.length && <option value="">暂无 Provider</option>}
                                            {config?.providers.map((provider) => (
                                                <option key={provider.id} value={provider.id}>
                                                    {providerDisplayName(provider)}
                                                </option>
                                            ))}
                                        </select>
                                    </Field>
                                </div>
                                <button
                                    className="management-primary"
                                    type="button"
                                    disabled={loading || saving}
                                    onClick={() => {
                                        setError('')
                                        setEditor({ kind: 'provider', index: null })
                                    }}
                                >
                                    <Plus size={16} aria-hidden="true" />
                                    <span>添加 Provider</span>
                                </button>
                            </div>
                        </div>
                    </header>
                    {error && (
                        <div className="management-error" role="alert">
                            <AlertCircle size={17} aria-hidden="true" />
                            <span>{error}</span>
                        </div>
                    )}
                </div>
                <div className="page model-config-content">
                    {loading ? (
                        <div className="management-loading" aria-label="加载配置中">
                            <LoaderCircle size={20} className="is-spinning" aria-hidden="true" />
                        </div>
                    ) : (
                        <ProviderDirectoryList
                            providers={config?.providers || []}
                            models={config?.models || []}
                            onEditProvider={(index) => {
                                setError('')
                                setEditor({ kind: 'provider', index })
                            }}
                            onEditModel={(index, providerID) => {
                                setError('')
                                setEditor({ kind: 'model', index, providerID })
                            }}
                            onAddModel={(providerID) => {
                                setError('')
                                setEditor({ kind: 'model', index: null, providerID })
                            }}
                        />
                    )}
                </div>
            </div>
            {editor?.kind === 'provider' && config && (
                <ProviderDialog
                    provider={editor.index === null ? null : config.providers[editor.index]}
                    modelCount={editor.index === null ? 0 : config.models.filter((model) => (
                        model.providerId === config.providers[editor.index!].id
                    )).length}
                    models={editor.index === null ? [] : config.models.filter((model) => (
                        model.providerId === config.providers[editor.index!].id
                    ))}
                    error={error}
                    saving={saving}
                    onSave={saveProvider}
                    onDelete={editor.index === null ? undefined : () => void deleteProvider(editor.index as number)}
                    onClose={() => !saving && setEditor(null)}
                />
            )}
            {editor?.kind === 'model' && config && modelEditorProvider && (
                <ModelDialog
                    model={editor.index === null ? null : config.models[editor.index]}
                    provider={modelEditorProvider}
                    groups={config.groups}
                    error={error}
                    saving={saving}
                    onSave={saveModel}
                    onDelete={editor.index === null ? undefined : () => void deleteModel(editor.index as number)}
                    onClose={() => !saving && setEditor(null)}
                />
            )}
        </section>
    )
}
