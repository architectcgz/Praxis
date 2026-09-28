import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react'
import { Brain, ChevronDown, Cpu, Mic, Plus, Send, Server, Square } from 'lucide-react'
import type { ModelOption } from '../../api'

type AgentTaskInputProps = {
    active: boolean
    closed: boolean
    input: string
    setInput: (value: string) => void
    models: ModelOption[]
    selectedProviderID: string
    selectedModelID: string
    reasoning: string
    busy: boolean
    onSend: () => void
    onControl: (kind: 'pause' | 'close') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
}

export function AgentTaskInput({ active, closed, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange }: AgentTaskInputProps) {
    const maxInputHeight = 220
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const providerPickerRef = useRef<HTMLDivElement>(null)
    const modelPickerRef = useRef<HTMLDivElement>(null)
    const reasoningPickerRef = useRef<HTMLDivElement>(null)
    const [providerPickerOpen, setProviderPickerOpen] = useState(false)
    const [modelPickerOpen, setModelPickerOpen] = useState(false)
    const [reasoningPickerOpen, setReasoningPickerOpen] = useState(false)
    const providerOptions = Array.from(new Map(models.map((model) => [model.providerId, model])).values())
    const providerModels = models.filter((model) => model.providerId === selectedProviderID)
    const selectedProvider = providerOptions.find((provider) => provider.providerId === selectedProviderID)
    const selectedModel = models.find((model) => model.providerId === selectedProviderID && model.modelId === selectedModelID)
    const reasoningAvailable = (selectedModel?.reasoningLevels.length || 0) > 0
    const reasoningLevels = selectedModel?.reasoningLevels || []
    const selectedReasoningIndex = Math.max(0, reasoningLevels.indexOf(reasoning))
    const selectedReasoning = reasoningLevels[selectedReasoningIndex] || '无推理'
    const reasoningProgress = reasoningLevels.length > 1
        ? `${(selectedReasoningIndex / (reasoningLevels.length - 1)) * 100}%`
        : '100%'

    useLayoutEffect(() => {
        const textarea = inputRef.current
        if (!textarea) {
            return
        }
        textarea.style.height = 'auto'
        const contentHeight = textarea.scrollHeight
        textarea.style.height = `${Math.min(contentHeight, maxInputHeight)}px`
        textarea.style.overflowY = contentHeight > maxInputHeight ? 'auto' : 'hidden'
    }, [input])

    useEffect(() => {
        if (!modelPickerOpen && !reasoningPickerOpen) {
            return
        }

        const closeOnOutsidePress = (event: PointerEvent) => {
            const target = event.target
            const clickedInsidePicker = target instanceof Node && (
                providerPickerRef.current?.contains(target) ||
                modelPickerRef.current?.contains(target) ||
                reasoningPickerRef.current?.contains(target)
            )
            if (!clickedInsidePicker) {
                setProviderPickerOpen(false)
                setModelPickerOpen(false)
                setReasoningPickerOpen(false)
            }
        }
        const closeOnEscape = (event: globalThis.KeyboardEvent) => {
            if (event.key === 'Escape') {
                setProviderPickerOpen(false)
                setModelPickerOpen(false)
                setReasoningPickerOpen(false)
            }
        }

        window.addEventListener('pointerdown', closeOnOutsidePress)
        window.addEventListener('keydown', closeOnEscape)
        return () => {
            window.removeEventListener('pointerdown', closeOnOutsidePress)
            window.removeEventListener('keydown', closeOnEscape)
        }
    }, [modelPickerOpen, reasoningPickerOpen])

    useEffect(() => {
        if (busy || active || models.length === 0) {
            setProviderPickerOpen(false)
            setModelPickerOpen(false)
        }
        if (busy || active || !reasoningAvailable) {
            setReasoningPickerOpen(false)
        }
    }, [busy, models.length, reasoningAvailable, active])

    const submitOrStop = useCallback(() => {
        if (active) {
            onControl('close')
            return
        }
        onSend()
    }, [active, onControl, onSend])

    const handleInputKeyDown = useCallback((event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) {
            return
        }
        event.preventDefault()
        submitOrStop()
    }, [submitOrStop])

    return (
        <div className="command-band">
            {closed && (
                <p className="round-notice">此代理已关闭。在此开始新一轮，或在目标更改时创建新会话。</p>
            )}
            <div className="composer-shell">
                <button
                    className="composer-icon-button"
                    type="button"
                    title="聚焦执行输入"
                    aria-label="聚焦执行输入"
                    onClick={() => inputRef.current?.focus()}
                    disabled={busy}
                >
                    <Plus size={18} strokeWidth={1.9} aria-hidden="true" />
                </button>
                <textarea
                    className="composer-textarea"
                    id="agent-input"
                    aria-label="执行输入"
                    ref={inputRef}
                    value={input}
                    onChange={(event) => setInput(event.target.value)}
                    onKeyDown={handleInputKeyDown}
                    placeholder={active ? '代理处于活动状态时输入不可用。' : closed ? '为此代理编写下一轮任务' : '为此代理编写新任务'}
                    disabled={active || busy}
                    rows={1}
                />
                <div className="composer-tools">
                    <div className={`composer-provider-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={providerPickerRef}>
                        <button
                            className="composer-provider-trigger"
                            type="button"
                            title={selectedProvider?.providerName || '没有配置的 Provider'}
                            aria-label={selectedProvider ? `Provider: ${selectedProvider.providerName}` : '没有配置的 Provider'}
                            aria-expanded={providerPickerOpen}
                            aria-controls="provider-picker-menu"
                            onClick={() => {
                                setModelPickerOpen(false)
                                setReasoningPickerOpen(false)
                                setProviderPickerOpen((open) => !open)
                            }}
                            disabled={busy || active || models.length === 0}
                        >
                            <Server size={15} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedProvider?.providerName || '无 Provider'}</span>
                            <ChevronDown className={`composer-select-chevron ${providerPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {providerPickerOpen && (
                            <div className="model-picker-menu" id="provider-picker-menu" aria-label="Provider 选择">
                                {providerOptions.map((provider) => (
                                    <button
                                        className={`model-picker-option ${provider.providerId === selectedProviderID ? 'is-selected' : ''}`}
                                        type="button"
                                        key={provider.providerId}
                                        aria-current={provider.providerId === selectedProviderID ? 'true' : undefined}
                                        onClick={() => {
                                            const providerModel = models.find((model) => model.providerId === provider.providerId && model.modelId === model.defaultModelId) ||
                                                models.find((model) => model.providerId === provider.providerId)
                                            if (providerModel) {
                                                onModelChange(providerModel.providerId, providerModel.modelId)
                                            }
                                            setProviderPickerOpen(false)
                                        }}
                                    >
                                        <span className="model-picker-option-label">{provider.providerName}</span>
                                        <span className="model-picker-option-provider">{provider.providerId}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                    <div className={`composer-model-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={modelPickerRef}>
                        <button
                            className="composer-model-trigger"
                            type="button"
                            title={selectedModel ? `${selectedModel.label} 通过 ${selectedModel.providerName}` : '没有配置的模型'}
                            aria-label={selectedModel ? `模型: ${selectedModel.label}` : '没有配置的模型'}
                            aria-expanded={modelPickerOpen}
                            aria-controls="model-picker-menu"
                            onClick={() => {
                                setReasoningPickerOpen(false)
                                setModelPickerOpen((open) => !open)
                            }}
                            disabled={busy || active || models.length === 0}
                        >
                            <Cpu size={15} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedModel?.label || '无模型'}</span>
                            <ChevronDown className={`composer-select-chevron ${modelPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {modelPickerOpen && (
                            <div className="model-picker-menu" id="model-picker-menu" aria-label="模型选择">
                                {providerModels.map((model) => (
                                    <button
                                        className={`model-picker-option ${model.providerId === selectedProviderID && model.modelId === selectedModelID ? 'is-selected' : ''}`}
                                        type="button"
                                        key={`${model.providerId}:${model.modelId}`}
                                        aria-current={model.providerId === selectedProviderID && model.modelId === selectedModelID ? 'true' : undefined}
                                        aria-label={`${model.label} 通过 ${model.providerName}${model.providerId === selectedProviderID && model.modelId === selectedModelID ? ', 已选中' : ''}`}
                                        title={`${model.label} 通过 ${model.providerName}`}
                                        onClick={() => {
                                            onModelChange(model.providerId, model.modelId)
                                            setModelPickerOpen(false)
                                        }}
                                    >
                                        <span className="model-picker-option-label">{model.label}</span>
                                        <span className="model-picker-option-provider">{model.providerName}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                    <div className={`composer-reasoning-picker ${reasoningAvailable ? '' : 'is-disabled'}`} ref={reasoningPickerRef}>
                        <button
                            className="composer-reasoning-trigger"
                            type="button"
                            title={reasoningAvailable ? `思考强度: ${selectedReasoning}` : '此模型不支持思考强度'}
                            aria-label={reasoningAvailable ? `思考强度: ${selectedReasoning}` : '此模型不支持思考强度'}
                            aria-expanded={reasoningPickerOpen}
                            aria-controls="thinking-intensity-picker"
                            onClick={() => {
                                setModelPickerOpen(false)
                                setReasoningPickerOpen((open) => !open)
                            }}
                            disabled={busy || active || !reasoningAvailable}
                        >
                            <Brain size={15} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedReasoning}</span>
                            <ChevronDown className={`composer-select-chevron ${reasoningPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {reasoningPickerOpen && (
                            <div className="thinking-intensity-picker" id="thinking-intensity-picker" role="group" aria-label="思考强度">
                                <output htmlFor="thinking-intensity-slider">{selectedReasoning}</output>
                                <input
                                    className="thinking-intensity-slider"
                                    id="thinking-intensity-slider"
                                    type="range"
                                    min="0"
                                    max={Math.max(0, reasoningLevels.length - 1)}
                                    step="1"
                                    value={selectedReasoningIndex}
                                    aria-label="思考强度"
                                    style={{ '--thinking-intensity-progress': reasoningProgress } as CSSProperties}
                                    onChange={(event) => onReasoningChange(reasoningLevels[Number(event.target.value)])}
                                />
                                <div
                                    className="thinking-intensity-labels"
                                    aria-hidden="true"
                                    style={{ '--thinking-level-count': reasoningLevels.length } as CSSProperties}
                                >
                                    {reasoningLevels.map((level) => <span key={level}>{level}</span>)}
                                </div>
                            </div>
                        )}
                    </div>
                    <button
                        className="composer-icon-button"
                        type="button"
                        title="语音输入不可用"
                        aria-label="语音输入不可用"
                        disabled
                    >
                        <Mic size={17} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                    <button
                        className={`composer-submit ${active ? 'composer-submit-danger' : ''}`}
                        type="button"
                        title={active ? '停止执行' : closed ? '开始新一轮' : '发送输入'}
                        aria-label={active ? '停止执行' : closed ? '开始新一轮' : '发送输入'}
                        onClick={submitOrStop}
                        disabled={busy || (!active && !input.trim())}
                    >
                        {active ? <Square size={16} strokeWidth={2.1} aria-hidden="true" /> : <Send size={16} strokeWidth={2.1} aria-hidden="true" />}
                    </button>
                </div>
            </div>
        </div>
    )
}
