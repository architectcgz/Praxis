import {
    FormEvent,
    ReactNode,
    useCallback,
    useEffect,
    useState,
} from "react";
import {
    AlertCircle,
    ArrowLeft,
    BrainCircuit,
    Check,
    ChevronDown,
    ChevronRight,
    Cpu,
    LoaderCircle,
    Pencil,
    Plus,
    RefreshCw,
    Server,
    Trash2,
    X,
    Settings,
} from "lucide-react";
import {
    getModelConfig,
    listProviderModels,
    ModelAPIFormat,
    ModelConfigDocument,
    ModelConfigOption,
    GroupConfigOption,
    ProviderConfigOption,
    saveModelConfig,
    setProviderKey,
} from "../../api";
import { readableError } from "../../shared/errors";
import { Overlay } from "../../components/ui";

type ModelConfigPanelProps = {
    onRefresh: () => void;
    onBack: () => void;
};

type Editor =
    | { kind: "provider"; index: number | null }
    | { kind: "model"; index: number | null; providerID: string };

type ModelDraft = Omit<ModelConfigOption, "apiFormat"> & {
    apiFormat: ModelAPIFormat | "";
};

function createProviderID(): string {
    return `provider-${crypto.randomUUID()}`;
}

function providerDisplayName(provider: ProviderConfigOption): string {
    return provider.providerName || provider.baseURL;
}

function modelKey(providerId: string, modelId: string): string {
    return `${providerId}\u0000${modelId}`;
}

function modelDraftWithID(current: ModelDraft, modelId: string): ModelDraft {
    return {
        ...current,
        modelId,
        label: current.label && current.label !== current.modelId ? current.label : modelId,
    };
}

export function ModelConfigPanel({ onRefresh, onBack }: ModelConfigPanelProps) {
    const [config, setConfig] = useState<ModelConfigDocument | null>(null);
    const [editor, setEditor] = useState<Editor | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState("");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            setConfig(await getModelConfig());
        } catch (reason) {
            setError(readableError(reason));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void load();
    }, [load]);

    const persist = async (next: ModelConfigDocument) => {
        setSaving(true);
        setError("");
        try {
            const result = await saveModelConfig(next);
            if (!result.saved) {
                setError(
                    result.validationError ||
                    "无法保存 Model 配置。",
                );
                return null;
            }
            const savedConfig = await getModelConfig();
            setConfig(savedConfig);
            onRefresh();
            setEditor(null);
            return savedConfig;
        } catch (reason) {
            setError(readableError(reason));
            return null;
        } finally {
            setSaving(false);
        }
    };

    const saveProvider = async (provider: ProviderConfigOption, key: string) => {
        if (!config || editor?.kind !== "provider") return;
        const providers = [...config.providers];
        if (editor.index === null) providers.push(provider);
        else providers[editor.index] = provider;
        const defaultProviderId = config.defaultProviderId || (config.providers.length === 0 ? provider.id : "");
        const saved = await persist({ ...config, defaultProviderId, providers });
        if (saved && key.trim()) {
            await setProviderKey(provider.id, key.trim());
        }
    };

    const saveDefaultProvider = async (defaultProviderId: string) => {
        if (!config || !defaultProviderId || defaultProviderId === config.defaultProviderId) return;
        await persist({ ...config, defaultProviderId });
    };

    const saveModel = async (model: ModelConfigOption) => {
        if (!config || editor?.kind !== "model") return;
        const nextModel = { ...model, providerId: editor.providerID };
        if (
            config.models.some(
                (item, index) => modelKey(item.providerId, item.modelId) === modelKey(nextModel.providerId, nextModel.modelId) && index !== editor.index,
            )
        ) {
            setError(`Model ID "${nextModel.modelId}" 已为此 Provider 配置。`);
            return;
        }
        const models = [...config.models];
        const previousModel = editor.index === null ? null : config.models[editor.index];
        if (editor.index === null) models.push(nextModel);
        else models[editor.index] = nextModel;
        const providers = config.providers.map((provider) => {
            if (provider.id !== editor.providerID) {
                return provider;
            }
            if (!provider.defaultModelId || previousModel?.modelId === provider.defaultModelId) {
                return { ...provider, defaultModelId: nextModel.modelId };
            }
            return provider;
        });
        await persist({ ...config, models, providers });
    };

    const deleteProvider = async (index: number) => {
        if (!config) return;
        const provider = config.providers[index];
        const providers = config.providers.filter((_, itemIndex) => itemIndex !== index);
        await persist({
            ...config,
            defaultProviderId: config.defaultProviderId === provider.id ? providers[0]?.id || "" : config.defaultProviderId,
            providers,
            models: config.models.filter((model) => model.providerId !== provider.id),
        });
    };

    const deleteModel = async (index: number) => {
        if (!config) return;
        const model = config.models[index];
        const remainingModels = config.models.filter((_, itemIndex) => itemIndex !== index);
        await persist({
            ...config,
            models: remainingModels,
            providers: config.providers.map((provider) => (
                provider.id === model.providerId && provider.defaultModelId === model.modelId
                    ? {
                        ...provider,
                        defaultModelId: remainingModels.find((item) => item.providerId === provider.id)?.modelId || "",
                    }
                    : provider
            )),
        });
    };
    const modelEditorProvider =
        editor?.kind === "model"
            ? config?.providers.find((provider) => provider.id === editor.providerID)
            : undefined;

    return (
        <section className="page page-models" aria-labelledby="model-config-title">
            <div className="page-header">
                <header className="model-config-heading">
                    <button className="back-link" type="button" onClick={onBack}>
                        <ArrowLeft size={16} aria-hidden="true" /> <span>设置</span>
                    </button>

                    <div className="model-config-header-content">
                        <div className="model-config-header-text">
                            <span className="empty-kicker">Provider 配置</span>
                            <h1 id="model-config-title">Provider 与 Model</h1>
                            <p>
                                首先添加 Provider，然后配置 Praxis 将在该 Provider 下使用的 Model。
                            </p>
                        </div>
                        <div className="model-config-header-actions">
                            <div className="model-config-default-provider">
                                <Field label="默认 Provider">
                                    <select
                                        value={config?.defaultProviderId || ""}
                                        disabled={loading || saving || !config?.providers.length}
                                        onChange={(event) => {
                                            void saveDefaultProvider(event.target.value);
                                        }}
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
                                    setError("");
                                    setEditor({ kind: "provider", index: null });
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

            <div className="scroll-region">
                {loading ? (
                    <div
                        className="management-loading"
                        aria-label="加载配置中"
                    >
                        <LoaderCircle
                            size={20}
                            className="is-spinning"
                            aria-hidden="true"
                        />
                    </div>
                ) : (
                    <ProviderDirectoryList
                        providers={config?.providers || []}
                        models={config?.models || []}
                        onEditProvider={(index) => {
                            setError("");
                            setEditor({ kind: "provider", index });
                        }}
                        onEditModel={(index, providerID) => {
                            setError("");
                            setEditor({ kind: "model", index, providerID });
                        }}
                        onAddModel={(providerID) => {
                            setError("");
                            setEditor({ kind: "model", index: null, providerID });
                        }}
                    />
                )}
            </div>

            {editor?.kind === "provider" && config && (
                <ProviderDialog
                    provider={
                        editor.index === null ? null : config.providers[editor.index]
                    }
                    modelCount={
                        editor.index === null
                            ? 0
                            : config.models.filter(
                                (model) =>
                                    model.providerId === config.providers[editor.index!].id,
                            ).length
                    }
                    models={
                        editor.index === null
                            ? []
                            : config.models.filter(
                                (model) =>
                                    model.providerId === config.providers[editor.index!].id,
                            )
                    }
                    error={error}
                    saving={saving}
                    onSave={saveProvider}
                    onDelete={
                        editor.index === null
                            ? undefined
                            : () => deleteProvider(editor.index as number)
                    }
                    onClose={() => !saving && setEditor(null)}
                />
            )}
            {editor?.kind === "model" && config && modelEditorProvider && (
                <ModelDialog
                    model={editor.index === null ? null : config.models[editor.index]}
                    provider={modelEditorProvider}
                    groups={config.groups}
                    error={error}
                    saving={saving}
                    onSave={saveModel}
                    onDelete={
                        editor.index === null
                            ? undefined
                            : () => deleteModel(editor.index as number)
                    }
                    onClose={() => !saving && setEditor(null)}
                />
            )}
        </section>
    );
}

function ProviderDirectoryList({
    providers,
    models,
    onEditProvider,
    onEditModel,
    onAddModel,
}: {
    providers: ProviderConfigOption[];
    models: ModelConfigOption[];
    onEditProvider: (index: number) => void;
    onEditModel: (index: number, providerID: string) => void;
    onAddModel: (providerID: string) => void;
}) {
    const [expandedProviders, setExpandedProviders] = useState<Set<string>>(
        () => new Set(providers.map(p => p.id))
    );

    const toggleProvider = (providerId: string) => {
        setExpandedProviders(prev => {
            const next = new Set(prev);
            if (next.has(providerId)) {
                next.delete(providerId);
            } else {
                next.add(providerId);
            }
            return next;
        });
    };

    if (!providers.length)
        return (
            <EmptyConfiguration
                icon={<Server size={22} />}
                title="没有连接的 Provider"
                detail="添加 Provider，然后配置您想使用的 Model。"
            />
        );
    return (
        <div className="provider-directory-list">
            {providers.map((provider, providerIndex) => {
                const providerModels = models.reduce<
                    Array<{ model: ModelConfigOption; index: number }>
                >((result, model, modelIndex) => {
                    if (model.providerId === provider.id) {
                        result.push({ model, index: modelIndex });
                    }
                    return result;
                }, []);
                const isExpanded = expandedProviders.has(provider.id);
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
                );
            })}
        </div>
    );
}

function ProviderModelItem({
    model,
    onEdit,
}: {
    model: ModelConfigOption;
    onEdit: () => void;
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
    );
}

function ProviderDialog({
    provider,
    modelCount,
    models,
    error,
    saving,
    onSave,
    onDelete,
    onClose,
}: {
    provider: ProviderConfigOption | null;
    modelCount: number;
    models: ModelConfigOption[];
    error: string;
    saving: boolean;
    onSave: (provider: ProviderConfigOption, key: string) => void;
    onDelete?: () => void;
    onClose: () => void;
}) {
    const [draft, setDraft] = useState<ProviderConfigOption>(
        () =>
            provider || {
                id: createProviderID(),
                providerName: "",
                baseURL: "",
                proxyURL: "",
                defaultModelId: "",
                hasAPIKey: false,
            },
    );
    const [apiKey, setApiKey] = useState("");
    const [confirmDelete, setConfirmDelete] = useState(false);
    const submit = (event: FormEvent) => {
        event.preventDefault();
        onSave({
            ...draft,
            providerName: draft.providerName.trim(),
            baseURL: draft.baseURL.trim(),
            proxyURL: draft.proxyURL.trim(),
            defaultModelId: draft.defaultModelId.trim(),
        }, apiKey);
    };
    return (
        <Overlay labelledBy="provider-dialog-title" onClose={onClose}>
            <section className="project-dialog config-dialog">
                <header className="dialog-heading">
                    <div>
                        <span className="empty-kicker">Provider</span>
                        <h2 id="provider-dialog-title">
                            {provider ? "编辑 Provider" : "添加 Provider"}
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
                            type="text"
                            value={apiKey}
                            onChange={(event) => setApiKey(event.target.value)}
                            placeholder={draft.hasAPIKey ? "已配置，留空保持不变" : "sk-..."}
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
                                : "此 Provider 将被移除。"
                        }
                    />
                </form>
            </section>
        </Overlay>
    );
}

function ModelDialog({
    model,
    provider,
    groups,
    error,
    saving,
    onSave,
    onDelete,
    onClose,
}: {
    model: ModelConfigOption | null;
    provider: ProviderConfigOption;
    groups: GroupConfigOption[];
    error: string;
    saving: boolean;
    onSave: (model: ModelConfigOption) => void;
    onDelete?: () => void;
    onClose: () => void;
}) {
    const [draft, setDraft] = useState<ModelDraft>(
        model
            ? { ...model, label: model.label || model.modelId }
            : {
                providerId: provider.id,
                modelId: "",
                label: "",
                groupId: groups[0]?.id || "",
                apiFormat: "",
                contextWindow: 128000,
                maxOutputTokens: 8192,
                reasoningLevels: ["low", "medium", "high"],
                defaultReasoningLevel: "medium",
            },
    );
    const [providerModels, setProviderModels] = useState<string[]>([]);
    const [providerModelsLoading, setProviderModelsLoading] = useState(false);
    const [providerModelsError, setProviderModelsError] = useState("");
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [validationError, setValidationError] = useState("");
    const loadProviderModels = useCallback(async () => {
        if (!provider.hasAPIKey) {
            setProviderModels([]);
            setProviderModelsError("");
            return;
        }
        setProviderModelsLoading(true);
        setProviderModelsError("");
        try {
            setProviderModels(await listProviderModels(provider.id));
        } catch (reason) {
            setProviderModels([]);
            setProviderModelsError(readableError(reason));
        } finally {
            setProviderModelsLoading(false);
        }
    }, [provider]);
    useEffect(() => {
        void loadProviderModels();
    }, [loadProviderModels]);
    const submit = (event: FormEvent) => {
        event.preventDefault();
        setValidationError("");

        // Custom validation
        if (!draft.label.trim()) {
            setValidationError("请填写显示名称");
            return;
        }
        if (!draft.modelId.trim()) {
            setValidationError("请填写 Model ID");
            return;
        }
        if (!draft.groupId.trim()) {
            setValidationError("请选择 Model 分组");
            return;
        }
        const apiFormat = draft.apiFormat;
        if (!apiFormat) {
            setValidationError("请选择 API Format");
            return;
        }
        if (draft.contextWindow < 2) {
            setValidationError("Context Window 必须大于等于 2");
            return;
        }
        if (draft.maxOutputTokens < 1) {
            setValidationError("最大输出 Tokens 必须大于等于 1");
            return;
        }

        onSave({
            ...draft,
            apiFormat,
            providerId: draft.providerId.trim(),
            modelId: draft.modelId.trim(),
            label: draft.label.trim(),
            defaultReasoningLevel: draft.reasoningLevels.includes(draft.defaultReasoningLevel)
                ? draft.defaultReasoningLevel
                : draft.reasoningLevels[0] || "",
        });
    };
    return (
        <Overlay labelledBy="model-dialog-title" onClose={onClose}>
            <section className="project-dialog config-dialog">
                <header className="dialog-heading">
                    <div>
                        <span className="empty-kicker">
                            {providerDisplayName(provider)} 的 Model
                        </span>
                        <h2 id="model-dialog-title">
                            {model ? "编辑 Model" : "添加 Model"}
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
                                        setValidationError("");
                                    }
                                }}
                                placeholder="GPT-5"
                                className={validationError === "请填写显示名称" ? "has-error" : ""}
                            />
                            {validationError === "请填写显示名称" && (
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
                                    apiFormat: event.target.value as ModelAPIFormat | "",
                                })
                            }
                            className={validationError === "请选择 API Format" ? "has-error" : ""}
                        >
                            <option value="">选择 API Format</option>
                            <option value="anthropic_messages">Anthropic Messages</option>
                            <option value="openai_responses">OpenAI Responses</option>
                            <option value="openai_chat_completions">
                                OpenAI Chat Completions
                            </option>
                        </select>
                        {validationError === "请选择 API Format" && (
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
                                            setValidationError("");
                                        }
                                    }}
                                    className={validationError === "请填写 Model ID" ? "has-error" : ""}
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
                                            setValidationError("");
                                        }
                                    }}
                                    placeholder="gpt-5"
                                    className={validationError === "请填写 Model ID" ? "has-error" : ""}
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
                                    className={providerModelsLoading ? "is-spinning" : ""}
                                    size={15}
                                    aria-hidden="true"
                                />
                            </button>
                        </div>
                        {validationError === "请填写 Model ID" && (
                            <span className="config-field-error">{validationError}</span>
                        )}
                        {provider.hasAPIKey && (
                            <span className="config-field-status" role="status">
                                {providerModelsLoading
                                    ? "加载可用 Models 中"
                                    : `${providerModels.length} 个可用 Model`}
                            </span>
                        )}
                    </Field>
                    <AdvancedOptions
                        draft={draft}
                        setDraft={setDraft}
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
    );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
    return (
        <label className="config-field">
            <span>{label}</span>
            {children}
        </label>
    );
}

function DialogActions({
    saving,
    onClose,
    onDelete,
    confirmDelete,
    setConfirmDelete,
    deleteDetail,
}: {
    saving: boolean;
    onClose: () => void;
    onDelete?: () => void;
    confirmDelete: boolean;
    setConfirmDelete: (value: boolean) => void;
    deleteDetail: string;
}) {
    return (
        <>
            {confirmDelete && (
                <div className="config-delete-confirm" role="alert">
                    <AlertCircle size={16} />
                    <span>{deleteDetail}</span>
                    <button type="button" disabled={saving} onClick={onDelete}>
                        确认删除
                    </button>
                </div>
            )}
            <div className="modal-actions config-actions">
                {onDelete && (
                    <button
                        className="config-delete"
                        type="button"
                        disabled={saving}
                        title="删除"
                        onClick={() => setConfirmDelete(!confirmDelete)}
                    >
                        <Trash2 size={15} aria-hidden="true" />
                        <span>删除</span>
                    </button>
                )}
                <span className="config-action-spacer" />
                <button
                    className="modal-secondary"
                    type="button"
                    disabled={saving}
                    onClick={onClose}
                >
                    取消
                </button>
                <button className="modal-primary" type="submit" disabled={saving}>
                    {saving ? (
                        <LoaderCircle className="is-spinning" size={15} />
                    ) : (
                        <Check size={15} />
                    )}
                    <span>保存</span>
                </button>
            </div>
        </>
    );
}

function EmptyConfiguration({
    icon,
    title,
    detail,
}: {
    icon: ReactNode;
    title: string;
    detail: string;
}) {
    return (
        <div className="provider-directory-empty">
            {icon}
            <div>
                <strong>{title}</strong>
                <p>{detail}</p>
            </div>
        </div>
    );
}

function formatAPIFormat(format: ModelAPIFormat) {
    switch (format) {
        case "anthropic_messages":
            return "Anthropic Messages";
        case "openai_responses":
            return "OpenAI Responses";
        case "openai_chat_completions":
            return "OpenAI Chat Completions";
    }
}

function AdvancedOptions({
    draft,
    setDraft,
}: {
    draft: ModelDraft;
    setDraft: (draft: ModelDraft) => void;
}) {
    const [isExpanded, setIsExpanded] = useState(false);

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
                    className={isExpanded ? "is-expanded" : ""}
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
                            value={draft.reasoningLevels.join(", ")}
                            onChange={(event) => {
                                const reasoningLevels = event.target.value
                                    .split(",")
                                    .map((level) => level.trim())
                                    .filter(Boolean);
                                setDraft({
                                    ...draft,
                                    reasoningLevels,
                                    defaultReasoningLevel: reasoningLevels.includes(draft.defaultReasoningLevel)
                                        ? draft.defaultReasoningLevel
                                        : reasoningLevels[0] || "",
                                });
                            }}
                            placeholder="low, medium, high, xhigh, max"
                        />
                    </Field>
                    {draft.reasoningLevels.length > 0 && (
                        <Field label="默认 Reasoning">
                            <select
                                required
                                value={draft.defaultReasoningLevel}
                                onChange={(event) =>
                                    setDraft({
                                        ...draft,
                                        defaultReasoningLevel: event.target.value,
                                    })
                                }
                            >
                                {draft.reasoningLevels.map((level) => (
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
    );
}
