import { LoaderCircle } from 'lucide-react'
import type { ToolStatus } from './toolPresentation'

export const statusLabels: Record<ToolStatus, string> = {
    queued: '等待执行', awaiting_approval: '等待审核', running: '进行中', completed: '已完成', failed: '失败', timeout: '超时',
    cancelled: '已取消', retrying: '重试中', blocked: '受阻', info: '信息', unknown: '结果未返回',
}

const statusTones: Record<ToolStatus, string> = {
    queued: '', awaiting_approval: 'warn', running: 'info', completed: 'ok', failed: 'bad', timeout: 'warn',
    cancelled: '', retrying: 'warn', blocked: 'warn', info: 'info', unknown: '',
}

/** 协作卡片共用的状态芯片；活动账本的行内状态由 ActivityLedger 的图标承载。 */
export function OutputStatus({ status, label }: { status: ToolStatus; label?: string }) {
    return <span className="output-chip" data-tone={statusTones[status]}>
        {status === 'running' || status === 'retrying' ? <LoaderCircle className="is-spinning" size={12} aria-hidden="true" /> : <span className="output-chip-dot" aria-hidden="true" />}
        {label || statusLabels[status]}
    </span>
}
