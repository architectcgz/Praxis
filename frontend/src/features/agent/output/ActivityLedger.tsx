import { Check, CircleAlert, Clock, LoaderCircle, ShieldAlert } from 'lucide-react'
import type { ReactNode } from 'react'
import { statusLabels } from './OutputStatus'
import type { ToolStatus } from './toolPresentation'

export type ActivityTone = 'done' | 'live' | 'wait' | 'bad' | 'idle'

/** 工具状态映射到四档视觉语义：已沉淀 / 正在发生 / 需要你 / 失败。 */
export function activityTone(status: ToolStatus): ActivityTone {
    if (status === 'completed') return 'done'
    if (status === 'running' || status === 'retrying') return 'live'
    if (status === 'awaiting_approval') return 'wait'
    if (status === 'failed' || status === 'timeout') return 'bad'
    return 'idle'
}

const toneIcons = {
    done: Check,
    live: LoaderCircle,
    wait: ShieldAlert,
    bad: CircleAlert,
    idle: Clock,
} as const

/** 进行中、失败与待批准默认展开，其余保持折叠，避免长会话被输出淹没。 */
export function isOpenByDefault(status: ToolStatus): boolean {
    return status === 'running' || status === 'failed' || status === 'timeout' || status === 'awaiting_approval'
}

/**
 * 活动账本的一行：折叠时是 28px 细行，展开后是仪器域面板。
 * 工具调用在正文列里保持发生顺序，因此这里仍是 details，不搬进侧栏。
 *
 * 右列只放一个值，避免「运行中 · 12s」这类组合把参数列挤成逐行抖动：
 * 异常状态（待批准 / 失败）显示状态词，其余显示耗时；没有计时记录时回落到
 * 状态词，不留空列。两者都由标记图标的可访问名称补齐，语义不丢失。
 */
export function ActivityItem({ tone, name, argument, status, statusLabel, duration, open = false, foot, children }: {
    tone: ActivityTone
    name: string
    argument: string
    status: ToolStatus
    statusLabel?: string
    duration?: ReactNode
    open?: boolean
    foot?: ReactNode
    children: ReactNode
}) {
    const Icon = toneIcons[tone]
    const label = statusLabel || statusLabels[status]
    const exceptional = tone === 'wait' || tone === 'bad'
    const meta = exceptional || !duration ? label : duration
    return <details className="act" data-tone={tone} open={open || undefined}>
        <summary className="act-row">
            <span className="act-name">{name}</span>
            <span className="act-arg" title={argument}>{argument}</span>
            <span className="act-time">{meta}</span>
            <span className="act-mark" role="img" aria-label={label} title={label}>
                <Icon size={13} aria-hidden="true" />
            </span>
        </summary>
        <div className="act-body instrument">
            {children}
            {foot && <div className="instrument-foot">{foot}</div>}
        </div>
    </details>
}
