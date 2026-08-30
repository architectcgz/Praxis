import { Children, isValidElement, useEffect, useLayoutEffect, useRef, useState, type ComponentPropsWithoutRef, type CSSProperties, type KeyboardEvent } from 'react'
import { Brain, Check, ChevronDown, Copy, Cpu, LoaderCircle, MessageSquare, Mic, Plus, Send, Square, Sparkles, Zap, FolderPlus } from 'lucide-react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { AgentHistoryItem, AgentSnapshot, ModelOption, SessionSnapshot } from '../../api'
import { StreamingOutput } from './types'

const markdownPlugins = [remarkGfm]
const markdownComponents: Components = {
    pre: MarkdownCodeBlock,
    table: MarkdownTable,
}

const codeLanguageLabels: Record<string, string> = {
    bash: 'Shell',
    css: 'CSS',
    go: 'Go',
    html: 'HTML',
    javascript: 'JavaScript',
    js: 'JavaScript',
    json: 'JSON',
    markdown: 'Markdown',
    md: 'Markdown',
    py: 'Python',
    python: 'Python',
    sh: 'Shell',
    shell: 'Shell',
    sql: 'SQL',
    ts: 'TypeScript',
    tsx: 'TSX',
    yaml: 'YAML',
    yml: 'YAML',
}

type SessionPanelProps = {
    loading: boolean
    session: SessionSnapshot | null
    sessionsCount: number
    agent: AgentSnapshot | null
    history: AgentHistoryItem[]
    streamingOutput: StreamingOutput | null
    awaitingOutput: boolean
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

export function SessionPanel({ loading, session, sessionsCount, agent, history, streamingOutput, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange }: SessionPanelProps) {
    if (!session) {
        if (loading) {
            return <section className="session-loading" aria-busy="true" aria-label="加载会话中" />
        }
        return (
            <div className="scroll-region">
                <section className="page session-empty-state">
                    <SessionEmptyState sessionsCount={sessionsCount} />
                </section>
            </div>
        )
    }

    return (
        <section className="session-panel">
            {loading ? (
                <div className="session-loading" aria-busy="true" aria-label="加载代理中" />
            ) : !agent ? (
                <>
                    <EmptySessionComposer input={input} setInput={setInput} busy={busy} onSend={onSend} />
                </>
            ) : (
                <>
                    <AgentConversation agent={agent} history={history} streamingOutput={streamingOutput} awaitingOutput={awaitingOutput} input={input} setInput={setInput} models={models} selectedProviderID={selectedProviderID} selectedModelID={selectedModelID} reasoning={reasoning} busy={busy} onSend={onSend} onControl={onControl} onModelChange={onModelChange} onReasoningChange={onReasoningChange} />
                </>
            )}
        </section>
    )
}

function EmptySessionComposer({ input, setInput, busy, onSend }: { input: string; setInput: (value: string) => void; busy: boolean; onSend: () => void }) {
    return <div className="agent-conversation">
        <div className="message-thread empty" aria-hidden="true" />
        <div className="command-band"><div className="composer-shell">
            <button className="composer-icon-button" type="button" title="聚焦执行输入" aria-label="聚焦执行输入" onClick={() => document.getElementById('agent-input')?.focus()} disabled={busy}><Plus size={18} strokeWidth={1.9} aria-hidden="true" /></button>
            <textarea className="composer-textarea" id="agent-input" aria-label="执行输入" value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); if (input.trim() && !busy) onSend() } }} placeholder="为主代理编写新任务" disabled={busy} rows={1} />
            <div className="composer-tools"><button className="composer-submit" type="button" title="发送输入" aria-label="发送输入" onClick={onSend} disabled={busy || !input.trim()}><Send size={16} strokeWidth={2.1} /></button></div>
        </div></div>
    </div>
}

type AgentConversationProps = {
    agent: AgentSnapshot
    history: AgentHistoryItem[]
    streamingOutput: StreamingOutput | null
    awaitingOutput: boolean
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

function AgentConversation({ agent, history, streamingOutput, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange }: AgentConversationProps) {
    const maxInputHeight = 220
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const modelPickerRef = useRef<HTMLDivElement>(null)
    const reasoningPickerRef = useRef<HTMLDivElement>(null)
    const previousAgentIDRef = useRef('')
    const followingOutputRef = useRef(true)
    const [modelPickerOpen, setModelPickerOpen] = useState(false)
    const [reasoningPickerOpen, setReasoningPickerOpen] = useState(false)
    const active = agent.state === 'executing' || agent.state === 'pausing'
    const waitingForOutput = awaitingOutput || (agent.state === 'executing' && !busy && !streamingOutput)
    const closed = agent.state === 'closed'
    const unavailable = active
    const selectedModel = models.find((model) => model.providerId === selectedProviderID && model.modelId === selectedModelID)
    const reasoningAvailable = Boolean(selectedModel?.reasoning.supported)
    const reasoningLevels = selectedModel?.reasoning.levels || []
    const selectedReasoningIndex = Math.max(0, reasoningLevels.indexOf(reasoning))
    const selectedReasoning = reasoningLevels[selectedReasoningIndex] || '无推理'
    const reasoningProgress = reasoningLevels.length > 1
        ? `${(selectedReasoningIndex / (reasoningLevels.length - 1)) * 100}%`
        : '100%'
    const orderedHistory = [...history].sort((left, right) => {
        const sequenceDifference = left.sequence - right.sequence
        if (sequenceDifference !== 0) {
            return sequenceDifference
        }
        return new Date(left.at).getTime() - new Date(right.at).getTime()
    })
    const latestItem = orderedHistory[orderedHistory.length - 1]
    const latestItemKey = latestItem
        ? `${latestItem.kind}-${latestItem.sequence}-${latestItem.at}-${latestItem.message?.content || latestItem.execution?.id || ''}`
        : ''

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const switchingAgent = previousAgentIDRef.current !== agent.id
        if (!switchingAgent && !followingOutputRef.current) {
            return
        }
        messageThread.scrollTo({
            top: messageThread.scrollHeight,
            behavior: 'auto',
        })
        followingOutputRef.current = true
        previousAgentIDRef.current = agent.id
    }, [agent.id, latestItemKey, streamingOutput?.content, waitingForOutput])

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
                modelPickerRef.current?.contains(target) || reasoningPickerRef.current?.contains(target)
            )
            if (!clickedInsidePicker) {
                setModelPickerOpen(false)
                setReasoningPickerOpen(false)
            }
        }
        const closeOnEscape = (event: globalThis.KeyboardEvent) => {
            if (event.key === 'Escape') {
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
        if (busy || unavailable || models.length === 0) {
            setModelPickerOpen(false)
        }
        if (busy || unavailable || !reasoningAvailable) {
            setReasoningPickerOpen(false)
        }
    }, [busy, models.length, reasoningAvailable, unavailable])

    const submitOrStop = () => {
        if (active) {
            onControl('close')
            return
        }
        onSend()
    }

    const handleInputKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) {
            return
        }
        event.preventDefault()
        submitOrStop()
    }

    const updateOutputFollowing = () => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        followingOutputRef.current = messageThread.scrollHeight - messageThread.scrollTop - messageThread.clientHeight <= 24
    }

    return (
        <div className="agent-conversation">
            <div
                className={`message-thread ${orderedHistory.length === 0 && !streamingOutput && !waitingForOutput ? 'empty' : ''}`}
                ref={messageThreadRef}
                aria-live="polite"
                onScroll={updateOutputFollowing}
            >
                {orderedHistory.length === 0 && !streamingOutput && !waitingForOutput ? (
                    <div className="message-empty" role="status">
                        <div className="message-empty-content">
                            <div className="message-empty-icon" aria-hidden="true">
                                <MessageSquare size={24} strokeWidth={1.6} />
                            </div>
                            <h3 className="message-empty-title">开始新对话</h3>
                            <p className="message-empty-text">
                                向 {agent.profile} 发送消息以开始协作。您可以提出问题、请求帮助或分配任务。
                            </p>
                        </div>
                    </div>
                ) : (
                    orderedHistory.map((item) => (
                        <HistoryItemView agent={agent} item={item} key={`${item.kind}-${item.sequence}-${item.message?.executionId || item.execution?.id || item.at}`} />
                    ))
                )}
                {streamingOutput && <StreamingOutputView agent={agent} content={streamingOutput.content} />}
                {waitingForOutput && <PendingOutputView agent={agent} />}
            </div>
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
                        disabled={unavailable || busy}
                        rows={1}
                    />
                    <div className="composer-tools">
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
                                disabled={busy || unavailable || models.length === 0}
                            >
                                <Cpu size={15} strokeWidth={1.8} aria-hidden="true" />
                                <span>{selectedModel?.label || '无模型'}</span>
                                <ChevronDown className={`composer-select-chevron ${modelPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                            </button>
                            {modelPickerOpen && (
                                <div className="model-picker-menu" id="model-picker-menu" aria-label="模型选择">
                                    {models.map((model) => (
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
                                disabled={busy || unavailable || !reasoningAvailable}
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
        </div>
    )
}

function StreamingOutputView({ agent, content }: { agent: AgentSnapshot; content: string }) {
    return (
        <article className="message message-assistant" aria-live="polite">
            <div className="message-meta">
                <strong>{agent.profile}</strong>
            </div>
            <MessageMarkdown content={content} />
        </article>
    )
}

function MessageMarkdown({ content }: { content: string }) {
    return (
        <div className="message-markdown">
            <ReactMarkdown components={markdownComponents} remarkPlugins={markdownPlugins} skipHtml>
                {content}
            </ReactMarkdown>
        </div>
    )
}

function MarkdownCodeBlock({ children }: ComponentPropsWithoutRef<'pre'>) {
    const code = Children.toArray(children)[0]
    if (!isValidElement<ComponentPropsWithoutRef<'code'>>(code)) {
        return <pre>{children}</pre>
    }
    const language = codeLanguageLabel(code.props.className)
    const content = typeof code.props.children === 'string'
        ? code.props.children.replace(/\n$/, '')
        : ''

    return (
        <div className="markdown-code-block">
            <div className="markdown-code-toolbar">
                <span className="markdown-code-language">{language}</span>
                <CopyIconButton
                    className="markdown-code-copy-button"
                    copiedLabel={`${language} 代码已复制`}
                    label={`复制 ${language} 代码`}
                    onCopy={() => copyText(content)}
                />
            </div>
            <pre>{code}</pre>
        </div>
    )
}

function MarkdownTable({ children }: ComponentPropsWithoutRef<'table'>) {
    const tableRef = useRef<HTMLTableElement>(null)

    return (
        <div className="markdown-table">
            <div className="markdown-table-toolbar">
                <span className="markdown-table-label">表格</span>
                <CopyIconButton
                    className="markdown-table-copy-button"
                    copiedLabel="表格已复制"
                    label="复制表格为制表符分隔文本"
                    onCopy={() => copyText(tableText(tableRef.current))}
                />
            </div>
            <div className="markdown-table-scroll">
                <table ref={tableRef}>{children}</table>
            </div>
        </div>
    )
}

function codeLanguageLabel(className: string | undefined) {
    const language = className?.match(/language-([a-z0-9+-]+)/i)?.[1]?.toLowerCase()
    if (!language) {
        return '纯文本'
    }
    return codeLanguageLabels[language] || language.toUpperCase()
}

function PendingOutputView({ agent }: { agent: AgentSnapshot }) {
    return (
        <article className="message message-assistant message-pending" role="status">
            <div className="message-meta">
                <strong>{agent.profile}</strong>
            </div>
            <div className="message-pending-status">
                <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" />
                <span>思考中</span>
            </div>
        </article>
    )
}

function HistoryItemView({ agent, item }: { agent: AgentSnapshot; item: AgentHistoryItem }) {
    if (item.kind === 'execution' && item.execution) {
        const content = failureMessage(item.execution.failureCode)
        return (
            <article className="message message-error">
                <div className="message-meta">
                    <strong>Praxis</strong>
                    <time dateTime={item.at} title={item.at}>{formatMessageTime(item.at)}</time>
                </div>
                <p>{content}</p>
                <MessageCopyButton content={content} />
            </article>
        )
    }
    if (!item.message) {
        return null
    }
    return (
        <article className={`message message-${item.message.role}`}>
            <div className="message-meta">
                <strong>{item.message.role === 'assistant' ? agent.profile : '你'}</strong>
                <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
            </div>
            {item.message.role === 'assistant' ? (
                <MessageMarkdown content={item.message.content} />
            ) : (
                <p>{item.message.content}</p>
            )}
            <MessageCopyButton content={item.message.content} />
        </article>
    )
}

function MessageCopyButton({ content }: { content: string }) {
    return (
        <div className="message-copy-actions">
            <CopyIconButton
                className="message-copy-button"
                copiedLabel="消息已复制"
                label="复制消息"
                onCopy={() => copyText(content)}
            />
        </div>
    )
}

type CopyIconButtonProps = {
    className: string
    copiedLabel: string
    label: string
    onCopy: () => Promise<void>
}

function CopyIconButton({ className, copiedLabel, label, onCopy }: CopyIconButtonProps) {
    const [copied, setCopied] = useState(false)

    useEffect(() => {
        if (!copied) {
            return
        }
        const timer = window.setTimeout(() => setCopied(false), 1600)
        return () => window.clearTimeout(timer)
    }, [copied])

    const copy = async () => {
        try {
            await onCopy()
            setCopied(true)
        } catch {
            setCopied(false)
        }
    }

    return (
        <button
            className={`${className} ${copied ? 'copied' : ''}`}
            type="button"
            title={copied ? copiedLabel : label}
            aria-label={copied ? copiedLabel : label}
            onClick={() => void copy()}
        >
            {copied ? <Check size={15} strokeWidth={2} aria-hidden="true" /> : <Copy size={15} strokeWidth={1.8} aria-hidden="true" />}
        </button>
    )
}

async function copyText(content: string) {
    if (!navigator.clipboard?.writeText) {
        throw new Error('Clipboard access is unavailable')
    }
    await navigator.clipboard.writeText(content)
}

function tableText(table: HTMLTableElement | null) {
    if (!table) {
        return ''
    }
    return Array.from(table.rows, (row) => (
        Array.from(row.cells, (cell) => cell.textContent?.replace(/\s+/g, ' ').trim() || '').join('\t')
    )).join('\n')
}

function failureMessage(code: string) {
    switch (code) {
        case 'provider_unavailable':
            return '代理无法响应，因为没有配置模型提供者。'
        case 'execution_provider_error':
            return '模型提供者无法完成请求。'
        case 'execution_tool_error':
            return '代理无法响应，因为必需的工具失败了。'
        case 'execution_policy_blocked':
            return '请求被配置的执行策略阻止。'
        case 'execution_approval_required':
            return '代理正在等待批准才能响应。'
        case 'execution_storage_error':
            return '代理无法响应，因为无法保存其持久状态。'
        case 'runtime_cancelled':
            return '执行在代理响应之前被取消。'
        case 'runtime_invalid_outcome':
            return '代理返回了无效的执行结果。'
        case 'runtime_failed':
            return '代理运行时在响应之前失败。'
        default:
            return `代理无法响应。失败代码: ${code}`
    }
}

function formatMessageTime(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return '未知时间'
    }
    return new Intl.DateTimeFormat(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    }).format(date)
}

function SessionEmptyState({ sessionsCount }: { sessionsCount: number }) {
    const hasExistingSessions = sessionsCount > 0

    return (
        <div className="session-empty-container">
            <div className="session-empty-hero">
                <div className="session-empty-icon-wrapper">
                    <div className="session-empty-icon-background" />
                    <div className="session-empty-icon-main">
                        <MessageSquare size={32} strokeWidth={1.6} />
                    </div>
                    <div className="session-empty-icon-accent">
                        <Sparkles size={18} strokeWidth={2} />
                    </div>
                </div>

                <h2 className="session-empty-title">
                    {hasExistingSessions ? '选择一个会话开始工作' : '开始您的第一次协作'}
                </h2>

                <p className="session-empty-description">
                    {hasExistingSessions
                        ? '从左侧面板中选择一个会话，查看对话历史并继续与 AI 代理协作。'
                        : '会话是您与 AI 代理协作的工作空间。在会话中，您可以与代理对话、执行任务、管理项目进度。'}
                </p>
            </div>

            <div className="session-empty-features">
                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <MessageSquare size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">智能对话</h3>
                        <p className="session-empty-feature-text">
                            与 AI 代理进行自然对话，获得即时帮助和建议
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <Zap size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">任务执行</h3>
                        <p className="session-empty-feature-text">
                            自动执行复杂任务，提高工作效率
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <FolderPlus size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">项目管理</h3>
                        <p className="session-empty-feature-text">
                            组织和跟踪多个项目的工作进展
                        </p>
                    </div>
                </div>
            </div>

            <div className="session-empty-action">
                {hasExistingSessions ? (
                    <div className="session-empty-hint">
                        <kbd className="session-empty-kbd">←</kbd>
                        <span>从左侧选择会话</span>
                    </div>
                ) : (
                    <div className="session-empty-hint">
                        <span>选择一个项目，然后点击</span>
                        <kbd className="session-empty-kbd">新建会话</kbd>
                        <span>开始</span>
                    </div>
                )}
            </div>
        </div>
    )
}
