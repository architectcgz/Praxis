import { memo, type ReactNode } from 'react'
import type { AgentHistoryItem, AgentMessageBlock } from '../../../api'
import { CopyIconButton, MessageMarkdown } from './MarkdownViews'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock } from './ToolCallViews'
import { MessageTiming } from '../../timing/OperationTimings'

const failureMessages: Record<string, string> = {
    provider_unavailable: '代理无法响应，因为没有配置模型提供者。',
    turn_provider_error: '模型提供者无法完成请求。',
    turn_tool_error: '代理无法响应，因为必需的工具失败了。',
    turn_policy_blocked: '请求被配置的执行策略阻止。',
    turn_approval_required: '代理正在等待批准才能响应。',
    turn_storage_error: '代理无法响应，因为无法保存其持久状态。',
    runtime_cancelled: '执行在代理响应之前被取消。',
    runtime_invalid_outcome: '代理返回了无效的执行结果。',
    runtime_failed: '代理运行时在响应之前失败。',
}

export const HistoryItemView = memo(function HistoryItemView({ agentProfile, item, toolResults, continuation = false, hasContinuation = false, copyContent }: {
    agentProfile: string
    item: AgentHistoryItem
    toolResults?: AgentMessageBlock[]
    continuation?: boolean
    hasContinuation?: boolean
    copyContent?: string
}) {
    if (item.kind === 'turn' && item.turn) {
        const content = item.turn.failureMessage || failureMessage(item.turn.failureCode)
        return (
            <article className="message message-error">
                <div className="message-meta">
                    <strong>Praxis</strong>
                    <time dateTime={item.at} title={item.at}>{formatMessageTime(item.at)}</time>
                </div>
                <p>{content}</p>
                <MessageActions content={content} />
            </article>
        )
    }
    if (!item.message) return null
    const messageBlocks = item.message.blocks || []
    const resultBlocks = messageBlocks.filter((block) => block.kind === 'tool_result')
    if (item.message.role === 'tool') {
        return (
            <article className="message message-tool">
                <div className="message-meta">
                    <strong>工具输出</strong>
                    <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
                </div>
                {resultBlocks.map((block, index) => <ToolResultBlock block={block} turnId={item.message!.turnId} key={`${block.callId}-${index}`} />)}
            </article>
        )
    }
    const messageRole = item.message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    const author = messageRole === 'assistant' ? agentProfile : messageRole === 'user' ? '你' : '未知来源'
    const contentToCopy = copyContent ?? item.message.content
    return (
        <article className={`message message-${messageClass}${hasContinuation ? ' message-continued' : ''}`}>
            {!continuation && <div className="message-meta">
                <strong>{author}</strong>
                <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
            </div>}
            {messageRole === 'assistant' ? (
                <>
                    {item.message.thinking && !messageBlocks.some((block) => block.kind === 'thinking') && <ThinkingDetails content={item.message.thinking} />}
                    {messageBlocks.length > 0 ? messageBlocks.map((block, index) => {
                        if (block.kind === 'thinking') return <ThinkingDetails content={block.text || ''} key={index} />
                        if (block.kind === 'text') return <MessageMarkdown content={block.text || ''} key={index} />
                        if (block.kind === 'tool_call') return (
                            <div className="message-tool-calls" key={index}>
                                <ToolCallBlock block={block} turnId={item.message!.turnId} result={toolResults?.find((result) => result.callId === block.callId)} />
                            </div>
                        )
                        return null
                    }) : item.message.content && <MessageMarkdown content={item.message.content} />}
                </>
            ) : item.message.content && <p>{item.message.content}</p>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {messageRole === 'assistant' && item.message.id && <MessageTiming turnId={item.message.turnId} referenceId={item.message.id} />}
            </MessageActions>
        </article>
    )
})

function MessageActions({ content, children }: { content: string; children?: ReactNode }) {
    return (
        <div className="message-copy-actions">
            {children}
            {content && <CopyIconButton
                className="message-copy-button"
                copiedLabel="消息已复制"
                label="复制消息"
                onCopy={() => copyText(content)}
            />}
        </div>
    )
}

async function copyText(content: string) {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard access is unavailable')
    await navigator.clipboard.writeText(content)
}

function failureMessage(code: string) {
    return failureMessages[code] || (code && code !== 'unknown' ? `代理无法响应。失败代码: ${code}` : '代理无法响应。')
}

let dateFormatter: Intl.DateTimeFormat | null = null

function formatMessageTime(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return '未知时间'
    }
    if (!dateFormatter) {
        dateFormatter = new Intl.DateTimeFormat(undefined, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        })
    }
    return dateFormatter.format(date)
}
