import { useCallback, useEffect, useState, type SubmitEvent } from 'react'
import {
    AlertCircle,
    ChevronRight,
    RefreshCw,
    Settings,
    X,
} from 'lucide-react'
import {
    listProviderModels,
    type GroupConfigOption,
    type ModelAPIFormat,
    type ModelConfigOption,
    type ProviderConfigOption,
} from '../../../api'
import { Overlay } from '../../../components/ui'
import { readableError } from '../../../shared/errors'
import { modelDraftWithID, providerDisplayName } from './modelConfigHelpers'
import { DialogActions, Field } from './ModelConfigDialogParts'
import type { ModelDraft } from './modelConfigTypes'

type ModelDialogProps = {
    model: ModelConfigOption | null
    provider: ProviderConfigOption
    groups: GroupConfigOption[]
    error: string
    saving: boolean
    onSave: (model: ModelConfigOption) => Promise<void>
    onDelete?: () => void
    onClose: () => void
}

export function ModelDialog({
    model,
    provider,
    groups,
    error,
    saving,
    onSave,
    onDelete,
    onClose,
}: ModelDialogProps) {
    const [draft, setDraft] = useState<ModelDraft>(
        model
            ? { ...model, label: model.label || model.modelId }
            : {
                providerId: provider.id,
                modelId: '',
                label: '',
                groupId: groups[0]?.id || '',
                apiFormat: '',
                contextWindow: 128000,
                maxOutputTokens: 8192,
                reasoningLevels: ['low', 'medium', 'high'],
                defaultReasoningLevel: 'medium',
            },
    )
    const [providerModels, setProviderModels] = useState<string[]>([])
    const [providerModelsLoading, setProviderModelsLoading] = useState(false)
    const [providerModelsError, setProviderModelsError] = useState('')
    const [confirmDelete, setConfirmDelete] = useState(false)
    const [validationError, setValidationError] = useState('')
    const [reasoningLevelsText, setReasoningLevelsText] = useState(() => draft.reasoningLevels.join(', '))

    const loadProviderModels = useCallback(async () => {
        if (!provider.hasAPIKey) {
            setProviderModels([])
            setProviderModelsError('')
            return
        }
        setProviderModelsLoading(true)
        setProviderModelsError('')
        try {
            setProviderModels(await listProviderModels(provider.id))
        } catch (reason) {
            setProviderModels([])
            setProviderModelsError(readableError(reason))
        } finally {
            setProviderModelsLoading(false)
        }
    }, [provider])

    useEffect(() => {
        void loadProviderModels()
    }, [loadProviderModels])

    const submit = (event: SubmitEvent) => {
        event.preventDefault()
        setValidationError('')

        if (!draft.label.trim()) {
            setValidationError('请填写显示名称')
            return
        }
        if (!draft.modelId.trim()) {
            setValidationError('请填写 Model ID')
            return
        }
        if (!draft.groupId.trim()) {
            setValidationError('请选择 Model 分组')
            return
        }
        const apiFormat = draft.apiFormat
        if (!apiFormat) {
            setValidationError('请选择 API Format')
            return
        }
        if (draft.contextWindow < 2) {
            setValidationError('Context Window 必须大于等于 2')
            return
        }
        if (draft.maxOutputTokens < 1) {
            setValidationError('最大输出 Tokens 必须大于等于 1')
            return
        }

        const reasoningLevels = parseReasoningLevels(reasoningLevelsText)
        void onSave({
            ...draft,
            apiFormat,
            providerId: draft.providerId.trim(),
            modelId: draft.modelId.trim(),
            label: draft.label.trim(),
            reasoningLevels,
            defaultReasoningLevel: reasoningLevels.includes(draft.defaultReasoningLevel)
                ? draft.defaultReasoningLevel
                : reasoningLevels[0] || '',
        })
    }

    return (
        <Overlay labelledBy="model-dialog-title" onClose={onClose}>
            <section className="project-dialog config-dialog">
                <header className="dialog-heading">
                    <div>
                        <span className="empty-kicker">
                            {providerDisplayName(provider)} 的 Model
                        </span>
                        <h2 id="model-dialog-title">
                            {model ? '编辑 Model' : '添加 Model'}
                        </h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="关闭"
                        aria-label="关闭"
                        disabled={saving}
                        onClick={onClose}
                    >
                        <X size={16} />
                    </button>
                </header>
                {error && (
                    <div className="management-error" role="alert">
                        <AlertCircle size={17} aria-hidden="true" />
                        <span>{error}</span>
                    </div>
                )}
                {providerModelsError && (
                    <div className="management-error" role="alert">
                        <AlertCircle size={17} aria-hidden="true" />
                        <span>{providerModelsError}</span>
                    </div>
                )}
                {validationError && (
                    <div className="management-error" role="alert">
                        <AlertCircle size={17} aria-hidden="true" />
                        <span>{validationError}</span>
                    </div>
                )}
                <form className="config-form" onSubmit={submit}>
                    <div className="config-form-grid">
                        <Field label="显示名称">
                            <input
                                value={draft.label}
                                onChange={(event) => {
                                    setDraft({ ...draft, label: event.target.value })
                                    if (validationError && event.target.value.trim()) {
                                        setValidationError('')
                                    }
                                }}
                                placeholder="GPT-5"
                                className={validationError === '请填写显示名称' ? 'has-error' : ''}
                            />
                            {validationError === '请填写显示名称' && (
                                <span className="config-field-error">{validationError}</span>
                            )}
                        </Field>
                    </div>
                    <Field label="Model 分组">
                        <select
                            required
                            value={draft.groupId}
                            onChange={(event) => setDraft({ ...draft, groupId: event.target.value })}
                        >
                            <option value="">选择分组</option>
                            {groups.map((group) => (
                                <option value={group.id} key={group.id}>{group.displayName}</option>
                            ))}
                        </select>
                    </Field>
                    <Field label="API Format">
                        <select
                            required
                            value={draft.apiFormat}
                            onChange={(event) =>
                                setDraft({
                                    ...draft,
                                    apiFormat: event.target.value as ModelAPIFormat | '',
                                })
                            }
                            className={validationError === '请选择 API Format' ? 'has-error' : ''}
                        >
                            <option value="">选择 API Format</option>
                            <option value="anthropic_messages">Anthropic Messages</option>
                            <option value="openai_responses">OpenAI Responses</option>
                            <option value="openai_chat_completions">
                                OpenAI Chat Completions
                            </option>
                        </select>
                        {validationError === '请选择 API Format' && (
                            <span className="config-field-error">{validationError}</span>
                        )}
                    </Field>
                    <Field label="Model ID">
                        <div className="config-model-picker">
                            {providerModels.length > 0 ? (
                                <select
                                    value={draft.modelId}
                                    onChange={(event) => {
                                        setDraft((current) => modelDraftWithID(current, event.target.value))
                                        if (validationError && event.target.value.trim()) {
                                            setValidationError('')
                                        }
                                    }}
                                    className={validationError === '请填写 Model ID' ? 'has-error' : ''}
                                >
                                    {!providerModels.includes(draft.modelId) && draft.modelId && (
                                        <option value={draft.modelId}>{draft.modelId}</option>
                                    )}
                                    <option value="">选择可用的 Model</option>
                                    {providerModels.map((providerModel) => (
                                        <option value={providerModel} key={providerModel}>{providerModel}</option>
                                    ))}
                                </select>
                            ) : (
                                <input
                                    value={draft.modelId}
                                    onChange={(event) => {
                                        const modelId = event.target.value
                                        setDraft((current) => modelDraftWithID(current, modelId))
                                        if (validationError && modelId.trim()) {
                                            setValidationError('')
                                        }
                                    }}
                                    placeholder="gpt-5"
                                    className={validationError === '请填写 Model ID' ? 'has-error' : ''}
                                />
                            )}
                            <button
                                className="icon-button config-model-refresh"
                                type="button"
                                title="刷新 Provider Models"
                                aria-label="刷新 Provider Models"
                                disabled={providerModelsLoading || !provider.hasAPIKey}
                                onClick={() => void loadProviderModels()}
                            >
                                <RefreshCw
                                    className={providerModelsLoading ? 'is-spinning' : ''}
                                    size={15}
                                    aria-hidden="true"
                                />
                            </button>
                        </div>
                        {validationError === '请填写 Model ID' && (
                            <span className="config-field-error">{validationError}</span>
                        )}
                        {provider.hasAPIKey && (
                            <span className="config-field-status" role="status">
                                {providerModelsLoading
                                    ? '加载可用 Models 中'
                                    : `${providerModels.length} 个可用 Model`}
                            </span>
                        )}
                    </Field>
                    <AdvancedOptions
                        draft={draft}
                        setDraft={setDraft}
                        reasoningLevelsText={reasoningLevelsText}
                        setReasoningLevelsText={setReasoningLevelsText}
                    />
                    <DialogActions
                        saving={saving}
                        onClose={onClose}
                        onDelete={onDelete}
                        confirmDelete={confirmDelete}
                        setConfirmDelete={setConfirmDelete}
                        deleteDetail="此 Model 将被移除。"
                    />
                </form>
            </section>
        </Overlay>
    )
}

function AdvancedOptions({
    draft,
    setDraft,
    reasoningLevelsText,
    setReasoningLevelsText,
}: {
    draft: ModelDraft
    setDraft: React.Dispatch<React.SetStateAction<ModelDraft>>
    reasoningLevelsText: string
    setReasoningLevelsText: (value: string) => void
}) {
    const [isExpanded, setIsExpanded] = useState(false)
    const reasoningLevels = parseReasoningLevels(reasoningLevelsText)
    const defaultReasoningLevel = reasoningLevels.includes(draft.defaultReasoningLevel)
        ? draft.defaultReasoningLevel
        : reasoningLevels[0] || ''

    return (
        <div className="config-advanced-section">
            <button
                className="config-advanced-toggle"
                type="button"
                onClick={() => setIsExpanded(!isExpanded)}
            >
                <Settings size={16} aria-hidden="true" />
                <span>高级选项</span>
                <ChevronRight
                    size={16}
                    aria-hidden="true"
                    className={isExpanded ? 'is-expanded' : ''}
                />
            </button>
            {isExpanded && (
                <div className="config-advanced-content">
                    <div className="config-form-grid">
                        <Field label="Context Window">
                            <input
                                required
                                min="2"
                                type="number"
                                value={draft.contextWindow}
                                onChange={(event) =>
                                    setDraft({
                                        ...draft,
                                        contextWindow: Number(event.target.value),
                                    })
                                }
                            />
                        </Field>
                        <Field label="最大输出 Tokens">
                            <input
                                required
                                min="1"
                                type="number"
                                value={draft.maxOutputTokens}
                                onChange={(event) =>
                                    setDraft({
                                        ...draft,
                                        maxOutputTokens: Number(event.target.value),
                                    })
                                }
                            />
                        </Field>
                    </div>
                    <Field label="Reasoning 级别">
                        <input
                            value={reasoningLevelsText}
                            onChange={(event) => setReasoningLevelsText(event.target.value)}
                            placeholder="low, medium, high, xhigh, max"
                        />
                    </Field>
                    {reasoningLevels.length > 0 && (
                        <Field label="默认 Reasoning">
                            <select
                                required
                                value={defaultReasoningLevel}
                                onChange={(event) =>
                                    setDraft({
                                        ...draft,
                                        defaultReasoningLevel: event.target.value,
                                    })
                                }
                            >
                                {reasoningLevels.map((level) => (
                                    <option key={level} value={level}>
                                        {level}
                                    </option>
                                ))}
                            </select>
                        </Field>
                    )}
                </div>
            )}
        </div>
    )
}

function parseReasoningLevels(value: string) {
    return [...new Set(value.split(',').map((level) => level.trim()).filter(Boolean))]
}
