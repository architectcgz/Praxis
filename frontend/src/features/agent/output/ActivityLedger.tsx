import { Check, ChevronRight, CircleAlert, Clock, LoaderCircle, ShieldAlert } from 'lucide-react'
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
 * 方案 A 统一细线账本的单行工具项：
 * 折叠时为紧凑自适应单行（图标动词 + 主参数 + 右侧结果微指标 + 耗时 + 状态图标），
 * 展开后衔接扁平仪器域视图。
 */
export function ActivityItem({
    tone,
    icon,
    name,
    rawName,
    argument,
    metrics,
    status,
    statusLabel,
    duration,
    open = false,
    foot,
    children,
}: {
    tone: ActivityTone
    icon?: ReactNode
    name: string
    rawName?: string
    argument: string
    metrics?: ReactNode
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
    return (
        <details className="act" data-tone={tone} open={open || undefined}>
            <summary className="act-row">
                <ChevronRight className="act-chevron" size={13} strokeWidth={2} aria-hidden="true" />
                <span className="act-verb" title={rawName || name}>
                    {icon}
                    <span>{name}</span>
                </span>
                <span className="act-arg" title={argument}>{argument}</span>
                {metrics && <span className="act-metrics">{metrics}</span>}
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
    )
}
