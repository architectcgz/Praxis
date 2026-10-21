import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react'
import { Brain, ChevronDown, Cpu, Plus, Send, Server, Square } from 'lucide-react'
import type { ModelOption } from '../../api'
import { useSlashCommandMenu } from './SlashCommandMenu'
import { SessionTokenUsage } from './SessionTokenUsage'
import { ApprovalModeSelect } from './ApprovalModeSelect'

type AgentTaskInputProps = {
    projectId: string
    sessionId: string
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
    onControl: (kind: 'pause' | 'cancel') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

export function AgentTaskInput({ projectId, sessionId, active, canQueue, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange, commands, onCommand }: AgentTaskInputProps) {
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const slash = useSlashCommandMenu(input, setInput, commands, onCommand, busy)
    const providerPickerRef = useRef<HTMLDivElement>(null)
    const modelPickerRef = useRef<HTMLDivElement>(null)
    const reasoningPickerRef = useRef<HTMLDivElement>(null)
    const approvalPickerRef = useRef<HTMLDivElement>(null)
    const [providerPickerOpen, setProviderPickerOpen] = useState(false)
    const [modelPickerOpen, setModelPickerOpen] = useState(false)
    const [reasoningPickerOpen, setReasoningPickerOpen] = useState(false)
    const [approvalPickerOpen, setApprovalPickerOpen] = useState(false)
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
    const closePickers = useCallback(() => {
        setProviderPickerOpen(false)
        setModelPickerOpen(false)
        setReasoningPickerOpen(false)
        setApprovalPickerOpen(false)
    }, [])

    useEffect(() => {
        // 解除禁用后恢复输入；用户已聚焦其他控件时不抢焦点。
        if (!busy && document.activeElement === document.body) {
            inputRef.current?.focus({ preventScroll: true })
        }
    }, [busy])

    useLayoutEffect(() => {
        const textarea = inputRef.current
        if (!textarea) {
            return
        }
        textarea.style.height = 'auto'
        const contentHeight = textarea.scrollHeight
        textarea.style.height = `${contentHeight}px`
        textarea.style.overflowY = 'auto'
    }, [input])

    useLayoutEffect(() => {
        if (!providerPickerOpen && !modelPickerOpen && !reasoningPickerOpen && !approvalPickerOpen) {
            return
        }

        const picker = providerPickerOpen ? providerPickerRef.current : modelPickerOpen ? modelPickerRef.current
            : reasoningPickerOpen ? reasoningPickerRef.current : approvalPickerRef.current
        const popup = picker?.querySelector<HTMLElement>('.model-picker-menu, .thinking-intensity-picker')
        const trigger = picker?.querySelector<HTMLButtonElement>('button')
        const region = picker?.closest<HTMLElement>('.main-panel')
        if (!picker || !popup || !trigger) return

        const updatePlacement = () => {
            const bounds = region?.getBoundingClientRect()
            const anchor = trigger.getBoundingClientRect()
            const left = Math.max(8, bounds?.left ?? 0) + 4
            const right = Math.min(window.innerWidth - 8, bounds?.right ?? window.innerWidth) - 4
            const top = Math.max(8, bounds?.top ?? 0) + 4
            const bottom = Math.min(window.innerHeight - 8, bounds?.bottom ?? window.innerHeight) - 4
            const above = Math.max(0, anchor.top - top - 8)
            const below = Math.max(0, bottom - anchor.bottom - 8)
            if (right <= left || anchor.bottom <= top || anchor.top >= bottom || Math.max(above, below) < 40) {
                closePickers()
                return
            }
            popup.style.maxWidth = `${right - left}px`
            const height = Math.min(300, popup.scrollHeight + popup.offsetHeight - popup.clientHeight)
            const upward = above >= height || above >= below
            popup.style.maxHeight = `${Math.min(300, upward ? above : below)}px`
            popup.style.top = upward ? 'auto' : 'calc(100% + 8px)'
            popup.style.bottom = upward ? 'calc(100% + 8px)' : 'auto'
            const x = Math.max(left, Math.min(anchor.left + (anchor.width - popup.offsetWidth) / 2, right - popup.offsetWidth))
            popup.style.left = `${x - picker.getBoundingClientRect().left}px`
            popup.style.visibility = 'visible'
        }
        // 在首次绘制前约束到所属 Region，避免祖先裁剪或跨栏遮挡。
        updatePlacement()
        const selected = popup.querySelector<HTMLElement>('[aria-current="true"]') || popup.querySelector<HTMLElement>('input, button')
        selected?.focus()

        const closeOnOutsidePress = (event: PointerEvent) => {
            const target = event.target
            if (target instanceof Node && !picker.contains(target)) {
                closePickers()
            }
        }
        const onKeyDown = (event: globalThis.KeyboardEvent) => {
            if (event.key === 'Escape' || event.key === 'Tab') {
                if (event.key === 'Escape') {
                    event.preventDefault()
                    event.stopPropagation()
                }
                trigger.focus({ preventScroll: true })
                closePickers()
                return
            }
            if (reasoningPickerOpen || !popup.contains(document.activeElement)) return
            const items = Array.from(popup.querySelectorAll<HTMLButtonElement>('button'))
            const index = items.indexOf(document.activeElement as HTMLButtonElement)
            const next = event.key === 'ArrowDown' ? (index + 1) % items.length
                : event.key === 'ArrowUp' ? (index - 1 + items.length) % items.length
                : event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : -1
            if (next >= 0) {
                event.preventDefault()
                items[next]?.focus()
            }
        }
        const closeOnFocusExit = (event: FocusEvent) => {
            if (event.target instanceof Node && !picker.contains(event.target)) closePickers()
        }
        const closeOnScroll = (event: Event) => {
            if (event.target instanceof Node && popup.contains(event.target)) return
            if (popup.contains(document.activeElement)) trigger.focus({ preventScroll: true })
            closePickers()
        }
        const observer = new ResizeObserver(updatePlacement)
        observer.observe(picker)
        if (region) observer.observe(region)

        window.addEventListener('resize', updatePlacement)
        document.addEventListener('pointerdown', closeOnOutsidePress, true)
        document.addEventListener('keydown', onKeyDown)
        document.addEventListener('focusin', closeOnFocusExit)
        document.addEventListener('scroll', closeOnScroll, true)
        return () => {
            observer.disconnect()
            window.removeEventListener('resize', updatePlacement)
            document.removeEventListener('pointerdown', closeOnOutsidePress, true)
            document.removeEventListener('keydown', onKeyDown)
            document.removeEventListener('focusin', closeOnFocusExit)
            document.removeEventListener('scroll', closeOnScroll, true)
        }
    }, [approvalPickerOpen, closePickers, modelPickerOpen, providerPickerOpen, reasoningPickerOpen])

    useEffect(() => {
        if (busy) setApprovalPickerOpen(false)
        if (busy || models.length === 0) {
            setProviderPickerOpen(false)
            setModelPickerOpen(false)
        }
        if (busy || !reasoningAvailable) {
            setReasoningPickerOpen(false)
        }
    }, [busy, models.length, reasoningAvailable])

    const sendDisabled = busy || !input.trim() || (active && !canQueue)

    const handleInputKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (slash.handleKeyDown(event) || event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return
        event.preventDefault()
        if (!sendDisabled) onSend()
    }

    return (
        <div className="command-band">
            {slash.menu}
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
                    role="combobox"
                    aria-autocomplete="list"
                    aria-controls={slash.menuOpen ? 'slash-command-menu' : undefined}
                    aria-expanded={slash.menuOpen}
                    aria-activedescendant={slash.selectedOptionID}
                    ref={inputRef}
                    value={input}
                    onChange={(event) => slash.changeInput(event.target.value)}
                    onKeyDown={handleInputKeyDown}
                    placeholder={canQueue ? '编写后续任务，发送后排队执行' : '为此代理编写新任务'}
                    disabled={busy}
                    rows={1}
                />
                <SessionTokenUsage key={sessionId} />
                <div className="composer-tools">
                    <div className={`composer-select-picker composer-provider-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={providerPickerRef}>
                        <button
                            className="composer-select-trigger composer-provider-trigger"
                            type="button"
                            title={selectedProvider?.providerName || '请选择 Provider'}
                            aria-label={selectedProvider ? `Provider: ${selectedProvider.providerName}` : '请选择 Provider'}
                            aria-expanded={providerPickerOpen}
                            aria-haspopup="menu"
                            aria-controls="provider-picker-menu"
                            onClick={() => {
                                closePickers()
                                setProviderPickerOpen(!providerPickerOpen)
                            }}
                            disabled={busy || models.length === 0}
                        >
                            <Server size={15} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedProvider?.providerName || '选择 Provider'}</span>
                            <ChevronDown className={`composer-select-chevron ${providerPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {providerPickerOpen && (
                            <div className="model-picker-menu" id="provider-picker-menu" role="menu" aria-label="Provider 选择">
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
                                            setProviderPickerOpen(false)
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
                            aria-controls="model-picker-menu"
                            onClick={() => {
                                closePickers()
                                setModelPickerOpen(!modelPickerOpen)
                            }}
                            disabled={busy || models.length === 0}
                        >
                            <Cpu size={15} strokeWidth={1.8} aria-hidden="true" />
                            <span>{selectedModel?.label || '选择模型'}</span>
                            <ChevronDown className={`composer-select-chevron ${modelPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                        </button>
                        {modelPickerOpen && (
                            <div className="model-picker-menu" id="model-picker-menu" role="menu" aria-label="模型选择">
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
                                            setModelPickerOpen(false)
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
                            aria-controls="thinking-intensity-picker"
                            onClick={() => {
                                closePickers()
                                setReasoningPickerOpen(!reasoningPickerOpen)
                            }}
                            disabled={busy || !reasoningAvailable}
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
                    <ApprovalModeSelect
                        key={projectId}
                        projectId={projectId}
                        disabled={busy}
                        pickerRef={approvalPickerRef}
                        open={approvalPickerOpen}
                        onToggle={() => {
                            closePickers()
                            setApprovalPickerOpen(!approvalPickerOpen)
                        }}
                        onClose={() => {
                            setApprovalPickerOpen(false)
                            approvalPickerRef.current?.querySelector('button')?.focus({ preventScroll: true })
                        }}
                    />
                    <div className="composer-send-controls">
                        {active && (
                            <button
                                className="composer-submit composer-submit-danger"
                                type="button"
                                title="停止当前执行"
                                aria-label="停止当前执行"
                                onClick={() => onControl('cancel')}
                                disabled={busy}
                            >
                                <Square size={16} strokeWidth={2.1} aria-hidden="true" />
                            </button>
                        )}
                        <button
                            className="composer-submit"
                            type="button"
                            title={canQueue ? '加入任务队列' : '发送输入'}
                            aria-label={canQueue ? '加入任务队列' : '发送输入'}
                            onClick={onSend}
                            disabled={sendDisabled}
                        >
                            <Send size={16} strokeWidth={2.1} aria-hidden="true" />
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
