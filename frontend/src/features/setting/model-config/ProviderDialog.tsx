import { useState, type SubmitEvent } from 'react'
import { AlertCircle, X } from 'lucide-react'
import type { ProviderConfigOption } from '../../../api'
import { Overlay } from '../../../components/ui'
import { createProviderID } from './modelConfigHelpers'
import { DialogActions, Field } from './ModelConfigDialogParts'
import type { ProviderDialogProps } from './modelConfigTypes'

export function ProviderDialog({
    provider,
    modelCount,
    models,
    error,
    saving,
    onSave,
    onDelete,
    onClose,
}: ProviderDialogProps) {
    const [draft, setDraft] = useState<ProviderConfigOption>(
        () => provider || {
            id: createProviderID(),
            providerName: '',
            baseURL: '',
            proxyURL: '',
            defaultModelId: '',
            hasAPIKey: false,
        },
    )
    const [apiKey, setApiKey] = useState('')
    const [confirmDelete, setConfirmDelete] = useState(false)

    const submit = (event: SubmitEvent) => {
        event.preventDefault()
        void onSave({
            ...draft,
            providerName: draft.providerName.trim(),
            baseURL: draft.baseURL.trim(),
            proxyURL: draft.proxyURL.trim(),
            defaultModelId: draft.defaultModelId.trim(),
        }, apiKey)
    }

    return (
        <Overlay labelledBy="provider-dialog-title" onClose={onClose}>
            <section className="project-dialog config-dialog">
                <header className="dialog-heading">
                    <div>
                        <span className="empty-kicker">Provider</span>
                        <h2 id="provider-dialog-title">
                            {provider ? '编辑 Provider' : '添加 Provider'}
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
                <form className="config-form" onSubmit={submit}>
                    <Field label="Provider 名称">
                        <input
                            value={draft.providerName}
                            onChange={(event) =>
                                setDraft({ ...draft, providerName: event.target.value })
                            }
                            placeholder="OpenAI"
                        />
                    </Field>
                    <Field label="Base URL">
                        <input
                            required
                            type="url"
                            value={draft.baseURL}
                            onChange={(event) =>
                                setDraft({ ...draft, baseURL: event.target.value })
                            }
                            placeholder="https://api.example.com"
                        />
                    </Field>
                    <Field label="Proxy URL (可选)">
                        <input
                            type="url"
                            value={draft.proxyURL}
                            onChange={(event) =>
                                setDraft({ ...draft, proxyURL: event.target.value })
                            }
                            placeholder="http://127.0.0.1:7897"
                        />
                    </Field>
                    <Field label="Model">
                        <select
                            value={draft.defaultModelId}
                            disabled={!models.length}
                            onChange={(event) =>
                                setDraft({ ...draft, defaultModelId: event.target.value })
                            }
                        >
                            {!models.length && <option value="">请先配置 Model</option>}
                            {models.length > 0 && <option value="">使用第一个 Model</option>}
                            {models.map((model) => (
                                <option key={model.modelId} value={model.modelId}>
                                    {model.label || model.modelId}
                                </option>
                            ))}
                        </select>
                    </Field>
                    <Field label="API Key">
                        <input
                            required={!draft.hasAPIKey}
                            type="password"
                            value={apiKey}
                            onChange={(event) => setApiKey(event.target.value)}
                            placeholder={draft.hasAPIKey ? '已配置，留空保持不变' : 'sk-...'}
                            autoComplete="off"
                        />
                    </Field>
                    <DialogActions
                        saving={saving}
                        onClose={onClose}
                        onDelete={onDelete}
                        confirmDelete={confirmDelete}
                        setConfirmDelete={setConfirmDelete}
                        deleteDetail={
                            modelCount
                                ? `这也会移除 ${modelCount} 个 Model。`
                                : '此 Provider 将被移除。'
                        }
                    />
                </form>
            </section>
        </Overlay>
    )
}
