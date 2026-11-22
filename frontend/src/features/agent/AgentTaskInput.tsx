import { useEffect, useId, useLayoutEffect, type CSSProperties, type KeyboardEvent, type RefObject } from 'react'
import { ArrowUp, Brain, ChevronDown, Cpu, ListPlus, Server, Square } from 'lucide-react'
import type { ModelOption, SessionApprovalMode } from '../../api'
import { useSlashCommandMenu } from './SlashCommandMenu'
import { ApprovalModeSelect } from './ApprovalModeSelect'
import { useInputPickers } from './useInputPickers'

type AgentTaskInputProps = {
    inputRef: RefObject<HTMLTextAreaElement | null>
    sessionId: string
    approvalMode: SessionApprovalMode | null
    onApprovalModeSaved: (settings: SessionApprovalMode) => void
    active: boolean
    canQueue: boolean
    input: string
    setInput: (value: string) => void
    models: ModelOption[]
    selectedProviderID: string
    selectedModelID: string
    reasoning: string
    busy: boolean
    onSend: () => void
    onStop: () => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

export function AgentTaskInput({
    inputRef,
    sessionId,
    approvalMode,
    onApprovalModeSaved,
    active,
    canQueue,
    input,
    setInput,
    models,
    selectedProviderID,
    selectedModelID,
    reasoning,
    busy,
    onSend,
    onStop,
    onModelChange,
    onReasoningChange,
    commands,
    onCommand,
}: AgentTaskInputProps) {
    const inputID = useId()
    const providerMenuID = useId()
    const modelMenuID = useId()
    const reasoningPickerID = useId()
    const reasoningSliderID = useId()
    const slash = useSlashCommandMenu(input, setInput, commands, onCommand, busy, inputRef)
    const providerOptions = Array.from(new Map(models.map((model) => [model.providerId, model])).values())
    const providerModels = models.filter((model) => !selectedProviderID || model.providerId === selectedProviderID)
    const selectedProvider = providerOptions.find((provider) => provider.providerId === selectedProviderID)
    const selectedModel = models.find((model) => model.providerId === selectedProviderID && model.modelId === selectedModelID)
    const reasoningAvailable = (selectedModel?.reasoningLevels.length || 0) > 0
    const reasoningLevels = selectedModel?.reasoningLevels || []
    const selectedReasoningIndex = Math.max(0, reasoningLevels.indexOf(reasoning))
    const selectedReasoning = reasoningLevels[selectedReasoningIndex] || '无推理'
    const reasoningProgress = reasoningLevels.length > 1
        ? `${(selectedReasoningIndex / (reasoningLevels.length - 1)) * 100}%`
        : '100%'
    const { openPicker, togglePicker, closePickers, providerPickerRef, modelPickerRef, reasoningPickerRef, approvalPickerRef } = useInputPickers({ busy, hasModels: models.length > 0, reasoningAvailable })
    const providerPickerOpen = openPicker === 'provider'
    const modelPickerOpen = openPicker === 'model'
    const reasoningPickerOpen = openPicker === 'reasoning'
    const approvalPickerOpen = openPicker === 'approval'

    useEffect(() => {
        if (!busy && inputRef.current?.getClientRects().length && document.activeElement === document.body) {
            inputRef.current.focus({ preventScroll: true })
        }
    }, [busy, inputRef])

    useLayoutEffect(() => {
        const textarea = inputRef.current
        if (!textarea) return
        textarea.style.height = 'auto'
        const contentHeight = textarea.scrollHeight
        const maxHeight = Number.parseFloat(window.getComputedStyle(textarea).maxHeight)
        if (Number.isFinite(maxHeight) && contentHeight > maxHeight) {
            textarea.style.height = `${maxHeight}px`
            textarea.style.overflowY = 'auto'
            return
        }
        textarea.style.height = `${contentHeight}px`
        textarea.style.overflowY = 'hidden'
    }, [input, inputRef])

    const sendDisabled = busy || !input.trim()

    const handleInputKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (slash.handleKeyDown(event) || event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return
        event.preventDefault()
        if (!sendDisabled) onSend()
    }

    return (
        <div className="command-band">
            {slash.menu}
            <div className="composer-shell">
                <textarea
                    className="composer-textarea"
                    id={inputID}
                    aria-label="执行输入"
                    role="combobox"
                    aria-autocomplete="list"
                    aria-controls={slash.menuOpen ? slash.menuID : undefined}
                    aria-expanded={slash.menuOpen}
                    aria-activedescendant={slash.selectedOptionID}
                    ref={inputRef}
                    value={input}
                    onChange={(event) => slash.changeInput(event.target.value)}
                    onKeyDown={handleInputKeyDown}
                    placeholder={canQueue ? '输入后续任务，发送后按 FIFO 排队执行（/ 触发命令）' : '向主代理下达任务或指令（/ 触发命令，Shift+Enter 换行）'}
                    disabled={busy}
                    rows={1}
                />
                <div className="composer-tools">
                    <div className={`composer-select-picker composer-provider-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={providerPickerRef}>
                        <button
                            className="composer-select-trigger composer-provider-trigger"
                            type="button"
                            title={selectedProvider?.providerName || '请选择 Provider'}
                            aria-label={selectedProvider ? `Provider: ${selectedProvider.providerName}` : '请选择 Provider'}
                            aria-expanded={providerPickerOpen}
                            aria-haspopup="menu"
                            aria-controls={providerPickerOpen ? providerMenuID : undefined}
                            onClick={() => togglePicker('provider')}
                            disabled={busy || models.length === 0}
                        >
                            <Server size={14} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedProvider?.providerName || '选择 Provider'}</span>
                            <ChevronDown className={`composer-select-chevron ${providerPickerOpen ? 'is-open' : ''}`} size={13} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {providerPickerOpen && (
                            <div className="model-picker-menu" id={providerMenuID} role="menu" aria-label="Provider 选择">
                                {providerOptions.map((provider) => (
                                    <button
                                        className={`model-picker-option ${provider.providerId === selectedProviderID ? 'is-selected' : ''}`}
                                        type="button"
                                        role="menuitem"
                                        tabIndex={-1}
                                        key={provider.providerId}
                                        aria-current={provider.providerId === selectedProviderID ? 'true' : undefined}
                                        onClick={() => {
                                            if (provider.providerId !== selectedProviderID) {
                                                onModelChange(provider.providerId, '')
                                            }
                                            closePickers()
                                            providerPickerRef.current?.querySelector('button')?.focus({ preventScroll: true })
                                        }}
                                    >
                                        <span className="model-picker-option-label">{provider.providerName}</span>
                                        <span className="model-picker-option-provider">{provider.providerId}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                    <div className={`composer-select-picker composer-model-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={modelPickerRef}>
                        <button
                            className="composer-select-trigger composer-model-trigger"
                            type="button"
                            title={selectedModel ? `${selectedModel.label} 通过 ${selectedModel.providerName}` : '请选择模型'}
                            aria-label={selectedModel ? `模型: ${selectedModel.label}` : '请选择模型'}
                            aria-expanded={modelPickerOpen}
                            aria-haspopup="menu"
                            aria-controls={modelPickerOpen ? modelMenuID : undefined}
                            onClick={() => togglePicker('model')}
                            disabled={busy || models.length === 0}
                        >
                            <Cpu size={14} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedModel?.label || '选择模型'}</span>
                            <ChevronDown className={`composer-select-chevron ${modelPickerOpen ? 'is-open' : ''}`} size={13} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {modelPickerOpen && (
                            <div className="model-picker-menu" id={modelMenuID} role="menu" aria-label="模型选择">
                                {providerModels.map((model) => (
                                    <button
                                        className={`model-picker-option ${model.providerId === selectedProviderID && model.modelId === selectedModelID ? 'is-selected' : ''}`}
                                        type="button"
                                        role="menuitem"
                                        tabIndex={-1}
                                        key={`${model.providerId}:${model.modelId}`}
                                        aria-current={model.providerId === selectedProviderID && model.modelId === selectedModelID ? 'true' : undefined}
                                        aria-label={`${model.label} 通过 ${model.providerName}${model.providerId === selectedProviderID && model.modelId === selectedModelID ? ', 已选中' : ''}`}
                                        title={`${model.label} 通过 ${model.providerName}`}
                                        onClick={() => {
                                            onModelChange(model.providerId, model.modelId)
                                            closePickers()
                                            modelPickerRef.current?.querySelector('button')?.focus({ preventScroll: true })
                                        }}
                                    >
                                        <span className="model-picker-option-label">{model.label}</span>
                                        <span className="model-picker-option-provider">{model.providerName}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                    <div className={`composer-select-picker composer-reasoning-picker ${reasoningAvailable ? '' : 'is-disabled'}`} ref={reasoningPickerRef}>
                        <button
                            className="composer-select-trigger composer-reasoning-trigger"
                            type="button"
                            title={reasoningAvailable ? `思考强度: ${selectedReasoning}` : '此模型不支持思考强度'}
                            aria-label={reasoningAvailable ? `思考强度: ${selectedReasoning}` : '此模型不支持思考强度'}
                            aria-expanded={reasoningPickerOpen}
                            aria-controls={reasoningPickerOpen ? reasoningPickerID : undefined}
                            onClick={() => togglePicker('reasoning')}
                            disabled={busy || !reasoningAvailable}
                        >
                            <Brain size={14} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedReasoning}</span>
                            <ChevronDown className={`composer-select-chevron ${reasoningPickerOpen ? 'is-open' : ''}`} size={13} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {reasoningPickerOpen && (
                            <div className="thinking-intensity-picker" id={reasoningPickerID} role="group" aria-label="思考强度">
                                <output htmlFor={reasoningSliderID}>{selectedReasoning}</output>
                                <input
                                    className="thinking-intensity-slider"
                                    id={reasoningSliderID}
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
                    <ApprovalModeSelect
                        key={sessionId}
                        sessionId={sessionId}
                        settings={approvalMode}
                        disabled={busy}
                        pickerRef={approvalPickerRef}
                        open={approvalPickerOpen}
                        onToggle={() => togglePicker('approval')}
                        onClose={() => {
                            closePickers()
                            approvalPickerRef.current?.querySelector('button')?.focus({ preventScroll: true })
                        }}
                        onSaved={onApprovalModeSaved}
                    />
                    <div className="composer-send-controls">
                        {active && (
                            <button
                                className="composer-submit composer-submit-danger"
                                type="button"
                                title="停止当前执行"
                                aria-label="停止当前执行"
                                onClick={onStop}
                                disabled={busy}
                            >
                                <Square size={14} strokeWidth={2.1} aria-hidden="true" />
                            </button>
                        )}
                        <button
                            className={`composer-submit ${canQueue ? 'composer-submit-queue' : ''}`}
                            type="button"
                            title={canQueue ? '加入任务队列' : '发送输入（Enter）'}
                            aria-label={canQueue ? '加入任务队列' : '发送输入'}
                            onClick={onSend}
                            disabled={sendDisabled}
                        >
                            {canQueue ? (
                                <>
                                    <ListPlus size={14} strokeWidth={2} aria-hidden="true" />
                                    <span>排队</span>
                                </>
                            ) : (
                                <ArrowUp size={16} strokeWidth={2.1} aria-hidden="true" />
                            )}
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
