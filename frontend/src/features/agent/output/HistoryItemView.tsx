import { memo, type ReactNode } from 'react'
import type { AgentHistoryItem, AgentMessageBlock } from '../../../api'
import { CopyIconButton, MessageMarkdown } from './MarkdownViews'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock } from './ToolCallViews'
import { useOperationTiming } from '../../timing/OperationTimings'
import { MessageTiming } from './MessageTiming'

const failureMessages: Record<string, string> = {
    provider_unavailable: '代理无法响应，因为没有配置模型提供者。',
    task_provider_error: '模型提供者无法完成请求。',
    task_tool_error: '代理无法响应，因为必需的工具失败了。',
    task_policy_blocked: '请求被配置的执行策略阻止。',
    task_approval_required: '代理正在等待批准才能响应。',
    task_storage_error: '代理无法响应，因为无法保存其持久状态。',
    runtime_cancelled: '执行在代理响应之前被取消。',
    request_canceled: '请求已取消。',
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
    if (item.kind === 'request_canceled' && item.task) {
        return <CanceledRequestView agentProfile={agentProfile} taskId={item.task.id} at={item.at} />
    }
    if (item.kind === 'task' && item.task) {
        const content = item.task.failureMessage || failureMessage(item.task.failureCode)
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
    const message = item.message
    const messageBlocks = message.blocks || []
    const resultBlocks = messageBlocks.filter((block) => block.kind === 'tool_result')
    if (message.role === 'tool') {
        return (
            <article className="message message-tool">
                <div className="message-meta">
                    <strong>工具输出</strong>
                    <time dateTime={message.at} title={message.at}>{formatMessageTime(message.at)}</time>
                </div>
                <div className="acts">
                    {resultBlocks.map((block, index) => <ToolResultBlock block={block} taskId={message.taskId} key={`${block.callId}-${index}`} />)}
                </div>
            </article>
        )
    }
    const messageRole = message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    const author = messageRole === 'assistant' ? agentProfile : messageRole === 'user' ? '你' : '未知来源'
    const contentToCopy = copyContent ?? message.content
    const segments = messageRole === 'assistant' ? groupAssistantBlocks(messageBlocks) : []
    return (
        <article className={`message message-${messageClass}${hasContinuation ? ' message-continued' : ''}`}>
            {!continuation && <div className="message-meta">
                <strong>{author}</strong>
                <time dateTime={message.at} title={message.at}>{formatMessageTime(message.at)}</time>
            </div>}
            {messageRole === 'assistant' ? (
                <>
                    {message.thinking && !messageBlocks.some((block) => block.kind === 'thinking') && <ThinkingDetails content={message.thinking} />}
                    {segments.length > 0 ? segments.map((segment) => {
                        if (segment.kind === 'thinking') return <ThinkingDetails content={segment.text} key={segment.key} />
                        if (segment.kind === 'text') return <MessageMarkdown content={segment.text} key={segment.key} />
                        // 一次运行里的连续工具调用合并成同一张活动账本。
                        return <div className="acts" key={segment.key}>
                            {segment.blocks.map((block, index) => <ToolCallBlock
                                block={block}
                                taskId={message.taskId}
                                result={toolResults?.find((result) => result.callId === block.callId)}
                                key={`${block.callId}-${index}`}
                            />)}
                        </div>
                    }) : message.content && <MessageMarkdown content={message.content} />}
                </>
            ) : message.content && <p>{message.content}</p>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {messageRole === 'assistant' && message.id && <MessageTiming taskId={message.taskId} referenceId={message.id} />}
            </MessageActions>
        </article>
    )
})

type AssistantSegment =
    | { key: string; kind: 'thinking'; text: string }
    | { key: string; kind: 'text'; text: string }
    | { key: string; kind: 'tools'; blocks: AgentMessageBlock[] }

/** 相邻的工具调用归入同一张活动账本，正文与思考保持原有先后。 */
function groupAssistantBlocks(blocks: AgentMessageBlock[]): AssistantSegment[] {
    const segments: AssistantSegment[] = []
    blocks.forEach((block, index) => {
        if (block.kind === 'thinking') {
            segments.push({ key: `thinking-${index}`, kind: 'thinking', text: block.text || '' })
            return
        }
        if (block.kind === 'text') {
            segments.push({ key: `text-${index}`, kind: 'text', text: block.text || '' })
            return
        }
        if (block.kind !== 'tool_call') return
        const last = segments[segments.length - 1]
        if (last?.kind === 'tools') {
            segments[segments.length - 1] = { ...last, blocks: [...last.blocks, block] }
            return
        }
        segments.push({ key: `tools-${index}`, kind: 'tools', blocks: [block] })
    })
    return segments
}

/** 取消记录来自持久化 Task；请求耗时单独查询，重启后保持相同状态。 */
function CanceledRequestView({ agentProfile, taskId, at }: { agentProfile: string; taskId: string; at: string }) {
    const timing = useOperationTiming(taskId, 'provider')
    return <article className="message message-assistant" role="status">
        <div className="message-meta">
            <strong>{agentProfile}</strong>
            <time dateTime={at} title={at}>{formatMessageTime(at)}</time>
        </div>
        <p>请求已取消</p>
        {timing?.referenceId && <MessageTiming taskId={taskId} referenceId={timing.referenceId} />}
    </article>
}

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
