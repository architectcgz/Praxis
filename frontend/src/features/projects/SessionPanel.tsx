import {Children, isValidElement, useEffect, useLayoutEffect, useRef, useState, type ComponentPropsWithoutRef, type CSSProperties, type KeyboardEvent} from 'react'
import {Brain, Check, ChevronDown, Copy, Cpu, LoaderCircle, MessageSquare, Mic, Plus, Send, Square} from 'lucide-react'
import ReactMarkdown, {type Components} from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {AgentHistoryItem, AgentSnapshot, ModelOption, SessionSnapshot} from '../../shared/api'
import {StreamingOutput} from './types'

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
	selectedModelID: string
	reasoning: string
    busy: boolean
    onSend: () => void
    onControl: (kind: 'pause' | 'close') => void
	onModelChange: (id: string) => void
	onReasoningChange: (level: string) => void
}

export function SessionPanel({loading, session, sessionsCount, agent, history, streamingOutput, awaitingOutput, input, setInput, models, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange}: SessionPanelProps) {
    if (!session) {
        if (loading) {
            return <section className="session-loading" aria-busy="true" aria-label="Loading session" />
        }
        return (
            <section className="empty-state">
                <span className="empty-kicker">Session catalog</span>
                <h1>{sessionsCount ? 'Select a Session' : 'No Sessions Yet'}</h1>
                <p>Choose a session from Projects to view its work and agents.</p>
            </section>
        )
    }

    return (
        <section className="session-panel">
			{loading ? (
				<div className="session-loading" aria-busy="true" aria-label="Loading agent" />
			) : !agent ? (
				<div className="empty-state">
					<span className="empty-kicker">{session.goal || 'Session'}</span>
					<h1>Select an Agent</h1>
					<p>Choose an agent from the right panel to continue this session.</p>
				</div>
			) : (
				<AgentConversation agent={agent} history={history} streamingOutput={streamingOutput} awaitingOutput={awaitingOutput} input={input} setInput={setInput} models={models} selectedModelID={selectedModelID} reasoning={reasoning} busy={busy} onSend={onSend} onControl={onControl} onModelChange={onModelChange} onReasoningChange={onReasoningChange} />
			)}
        </section>
    )
}

type AgentConversationProps = {
    agent: AgentSnapshot
    history: AgentHistoryItem[]
	streamingOutput: StreamingOutput | null
	awaitingOutput: boolean
    input: string
    setInput: (value: string) => void
	models: ModelOption[]
	selectedModelID: string
	reasoning: string
    busy: boolean
    onSend: () => void
    onControl: (kind: 'pause' | 'close') => void
	onModelChange: (id: string) => void
	onReasoningChange: (level: string) => void
}

function AgentConversation({agent, history, streamingOutput, awaitingOutput, input, setInput, models, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange}: AgentConversationProps) {
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
	const selectedModel = models.find((model) => model.id === selectedModelID)
	const reasoningAvailable = Boolean(selectedModel?.reasoning.supported)
    const reasoningLevels = selectedModel?.reasoning.levels || []
    const selectedReasoningIndex = Math.max(0, reasoningLevels.indexOf(reasoning))
    const selectedReasoning = reasoningLevels[selectedReasoningIndex] || 'No reasoning'
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
                        <span className="message-empty-icon" aria-hidden="true">
                            <MessageSquare size={19} strokeWidth={1.8} />
                        </span>
                        <span>No messages yet.</span>
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
                    <p className="round-notice">This Agent is closed. Start a new round here, or create a new Session when the goal changes.</p>
                )}
                <div className="composer-shell">
                    <button
                        className="composer-icon-button"
                        type="button"
                        title="Focus execution input"
                        aria-label="Focus execution input"
                        onClick={() => inputRef.current?.focus()}
                        disabled={busy}
                    >
                        <Plus size={18} strokeWidth={1.9} aria-hidden="true" />
                    </button>
                    <textarea
                        className="composer-textarea"
                        id="agent-input"
                        aria-label="Execution input"
                        ref={inputRef}
                        value={input}
                        onChange={(event) => setInput(event.target.value)}
                        onKeyDown={handleInputKeyDown}
                        placeholder={active ? 'Input is unavailable while this Agent is active.' : closed ? 'Write the next round task for this Agent' : 'Write a new task for this Agent'}
                        disabled={unavailable || busy}
                        rows={1}
                    />
                    <div className="composer-tools">
                        <div className={`composer-model-picker ${models.length === 0 ? 'is-disabled' : ''}`} ref={modelPickerRef}>
                            <button
                                className="composer-model-trigger"
                                type="button"
                                title={selectedModel ? `${selectedModel.label} via ${selectedModel.providerLabel}` : 'No configured models'}
                                aria-label={selectedModel ? `Model: ${selectedModel.label}` : 'No configured models'}
                                aria-expanded={modelPickerOpen}
                                aria-controls="model-picker-menu"
                                onClick={() => {
                                    setReasoningPickerOpen(false)
                                    setModelPickerOpen((open) => !open)
                                }}
                                disabled={busy || unavailable || models.length === 0}
                            >
                                <Cpu size={15} strokeWidth={1.8} aria-hidden="true" />
                                <span>{selectedModel?.label || 'No models'}</span>
                                <ChevronDown className={`composer-select-chevron ${modelPickerOpen ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
                            </button>
                            {modelPickerOpen && (
                                <div className="model-picker-menu" id="model-picker-menu" aria-label="Model choices">
                                    {models.map((model) => (
                                        <button
                                            className={`model-picker-option ${model.id === selectedModelID ? 'is-selected' : ''}`}
                                            type="button"
                                            key={model.id}
                                            aria-current={model.id === selectedModelID ? 'true' : undefined}
                                            aria-label={`${model.label} via ${model.providerLabel}${model.id === selectedModelID ? ', selected' : ''}`}
                                            title={`${model.label} via ${model.providerLabel}`}
                                            onClick={() => {
                                                onModelChange(model.id)
                                                setModelPickerOpen(false)
                                            }}
                                        >
                                            <span className="model-picker-option-label">{model.label}</span>
                                            <span className="model-picker-option-provider">{model.providerLabel}</span>
                                        </button>
                                    ))}
                                </div>
                            )}
                        </div>
                        <div className={`composer-reasoning-picker ${reasoningAvailable ? '' : 'is-disabled'}`} ref={reasoningPickerRef}>
                            <button
                                className="composer-reasoning-trigger"
                                type="button"
                                title={reasoningAvailable ? `Thinking intensity: ${selectedReasoning}` : 'This model does not support thinking intensity'}
                                aria-label={reasoningAvailable ? `Thinking intensity: ${selectedReasoning}` : 'This model does not support thinking intensity'}
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
                                <div className="thinking-intensity-picker" id="thinking-intensity-picker" role="group" aria-label="Thinking intensity">
                                    <output htmlFor="thinking-intensity-slider">{selectedReasoning}</output>
                                    <input
                                        className="thinking-intensity-slider"
                                        id="thinking-intensity-slider"
                                        type="range"
                                        min="0"
                                        max={Math.max(0, reasoningLevels.length - 1)}
                                        step="1"
                                        value={selectedReasoningIndex}
                                        aria-label="Thinking intensity"
                                        style={{'--thinking-intensity-progress': reasoningProgress} as CSSProperties}
                                        onChange={(event) => onReasoningChange(reasoningLevels[Number(event.target.value)])}
                                    />
                                    <div
                                        className="thinking-intensity-labels"
                                        aria-hidden="true"
                                        style={{'--thinking-level-count': reasoningLevels.length} as CSSProperties}
                                    >
                                        {reasoningLevels.map((level) => <span key={level}>{level}</span>)}
                                    </div>
                                </div>
                            )}
                        </div>
                        <button
                            className="composer-icon-button"
                            type="button"
                            title="Voice input unavailable"
                            aria-label="Voice input unavailable"
                            disabled
                        >
                            <Mic size={17} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                        <button
                            className={`composer-submit ${active ? 'composer-submit-danger' : ''}`}
                            type="button"
                            title={active ? 'Stop execution' : closed ? 'Start new round' : 'Send input'}
                            aria-label={active ? 'Stop execution' : closed ? 'Start new round' : 'Send input'}
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

function StreamingOutputView({agent, content}: {agent: AgentSnapshot; content: string}) {
	return (
		<article className="message message-assistant" aria-live="polite">
			<div className="message-meta">
				<strong>{agent.profile}</strong>
			</div>
			<MessageMarkdown content={content} />
		</article>
	)
}

function MessageMarkdown({content}: {content: string}) {
	return (
		<div className="message-markdown">
			<ReactMarkdown components={markdownComponents} remarkPlugins={markdownPlugins} skipHtml>
				{content}
			</ReactMarkdown>
		</div>
	)
}

function MarkdownCodeBlock({children}: ComponentPropsWithoutRef<'pre'>) {
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
					copiedLabel={`${language} code copied`}
					label={`Copy ${language} code`}
					onCopy={() => copyText(content)}
				/>
			</div>
			<pre>{code}</pre>
		</div>
	)
}

function MarkdownTable({children}: ComponentPropsWithoutRef<'table'>) {
	const tableRef = useRef<HTMLTableElement>(null)

	return (
		<div className="markdown-table">
			<div className="markdown-table-toolbar">
				<span className="markdown-table-label">Table</span>
				<CopyIconButton
					className="markdown-table-copy-button"
					copiedLabel="Table copied"
					label="Copy table as tab-separated text"
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
		return 'Plain text'
	}
	return codeLanguageLabels[language] || language.toUpperCase()
}

function PendingOutputView({agent}: {agent: AgentSnapshot}) {
	return (
		<article className="message message-assistant message-pending" role="status">
			<div className="message-meta">
				<strong>{agent.profile}</strong>
			</div>
			<div className="message-pending-status">
				<LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" />
				<span>Thinking</span>
			</div>
		</article>
	)
}

function HistoryItemView({agent, item}: {agent: AgentSnapshot; item: AgentHistoryItem}) {
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
                <strong>{item.message.role === 'assistant' ? agent.profile : 'You'}</strong>
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

function MessageCopyButton({content}: {content: string}) {
	return (
		<div className="message-copy-actions">
			<CopyIconButton
				className="message-copy-button"
				copiedLabel="Message copied"
				label="Copy message"
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

function CopyIconButton({className, copiedLabel, label, onCopy}: CopyIconButtonProps) {
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
        return 'The Agent could not respond because no model provider is configured.'
    case 'execution_provider_error':
        return 'The model provider could not complete the request.'
    case 'execution_tool_error':
        return 'The Agent could not respond because a required tool failed.'
    case 'execution_policy_blocked':
        return 'The request was blocked by the configured execution policy.'
    case 'execution_approval_required':
        return 'The Agent is waiting for approval before it can respond.'
    case 'execution_storage_error':
        return 'The Agent could not respond because its durable state could not be saved.'
    case 'runtime_cancelled':
        return 'The execution was cancelled before the Agent could respond.'
    case 'runtime_invalid_outcome':
        return 'The Agent returned an invalid execution result.'
    case 'runtime_failed':
        return 'The Agent runtime failed before it could respond.'
    default:
        return `The Agent could not respond. Failure code: ${code}`
    }
}

function formatMessageTime(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return 'Unknown time'
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
