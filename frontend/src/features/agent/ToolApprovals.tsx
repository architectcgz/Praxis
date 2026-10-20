import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { AlertCircle, Check, LoaderCircle, ShieldAlert, X } from 'lucide-react'
import { approveToolCall, denyToolCall, listToolApprovals, subscribeAgentEvents, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'
import { presentTool } from './output/toolPresentation'

type ApprovalState = {
    pendingApprovals: ToolApproval[]
    busy: string
    error: string
    reload: () => Promise<void>
    decide: (pendingApproval: ToolApproval, approve: boolean) => Promise<void>
}

const ToolApprovalsContext = createContext<ApprovalState | null>(null)

/** 会话内只查询和订阅一次审批状态，由审批闸口消费；重连后仍可审核同一个调用。 */
export function ToolApprovalsProvider({ agentId, children }: { agentId: string; children: ReactNode }) {
    const [pendingApprovals, setPendingApprovals] = useState<ToolApproval[]>([])
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const requestGeneration = useRef(0)
    const reload = useCallback(async () => {
        const current = ++requestGeneration.current
        try {
            const values = await listToolApprovals(agentId)
            if (current === requestGeneration.current) {
                setPendingApprovals(values)
                setError('')
            }
        } catch (err) {
            if (current === requestGeneration.current) setError(readableError(err))
        }
    }, [agentId])
    useEffect(() => {
        const unsubscribe = subscribeAgentEvents((event) => {
            if (event.agentId === agentId && ['tool_approval_required', 'tool_result', 'execution_ended', 'request_canceled'].includes(event.kind)) void reload()
        })
        void reload()
        return () => { requestGeneration.current += 1; unsubscribe() }
    }, [agentId, reload])
    const decide = async (pendingApproval: ToolApproval, approve: boolean) => {
        setBusy(pendingApproval.callId)
        setError('')
        try {
            if (approve) await approveToolCall(pendingApproval)
            else await denyToolCall(pendingApproval)
            await reload()
        } catch (err) { setError(readableError(err)) }
        finally { setBusy('') }
    }
    return <ToolApprovalsContext value={{ pendingApprovals, busy, error, reload, decide }}>{children}</ToolApprovalsContext>
}

/** 按任务与调用身份关联审核，供活动账本读取后端冻结的参数。 */
export function useToolApproval(executionId: string, callId?: string) {
    const approvals = useContext(ToolApprovalsContext)
    return approvals ? { ...approvals, pendingApproval: approvals.pendingApprovals.find((pendingApproval) => pendingApproval.executionId === executionId && pendingApproval.callId === callId) } : null
}

/**
 * 审批闸口：固定在输入区上方，一次只显示一条，批准后自动露出下一条。
 * 它是对「不处理就没法继续」的闸口，所以放在你本来就要操作的位置，
 * 而不是散落在长会话上游的卡片里。
 */
export function ApprovalGate() {
    const approvals = useContext(ToolApprovalsContext)
    const pending = approvals?.pendingApprovals[0]
    if (!approvals || !pending) return null
    const total = approvals.pendingApprovals.length
    const busy = approvals.busy === pending.callId
    const disabled = approvals.busy !== ''
    const presentation = presentTool(pending.name, pending.arguments)
    const summary = presentation.summary
    const path = typeof presentation.input.path === 'string' ? presentation.input.path : ''
    return <section className="approval-gate" aria-label="待批准的操作" aria-busy={busy}>
        <div className="approval-gate-box">
            <div className="approval-gate-head">
                <ShieldAlert size={13} aria-hidden="true" />
                <strong>需要批准</strong>
                <span className="approval-gate-note" title={summary}>{summary}</span>
                {total > 1 && <span className="approval-gate-count">{1} / {total}</span>}
            </div>
            <dl className="approval-gate-details">
                <div><dt>工具</dt><dd>{pending.name}</dd></div>
                {path && <div><dt>操作路径</dt><dd><code>{path}</code></dd></div>}
                <div><dt>命中规则</dt><dd>{pending.ruleId === 'default' ? '项目或全局默认' : pending.ruleId}</dd></div>
                <div><dt>处理方式</dt><dd>{pending.action === 'auto' ? '模型不确定，转人工' : '必须人工确认'}</dd></div>
                {pending.riskVerdict && <div><dt>模型审核</dt><dd>{pending.riskReason || pending.riskVerdict}</dd></div>}
            </dl>
            {approvals.error && <p className="approval-gate-error" role="alert">
                <AlertCircle size={13} aria-hidden="true" /><span>{approvals.error}</span>
            </p>}
            <div className="approval-gate-foot">
                <span className="approval-gate-hint" role="status">
                    {busy ? <><LoaderCircle className="is-spinning" size={13} aria-hidden="true" />正在提交审核…</> : '仅授权本次调用'}
                </span>
                <button className="approval-gate-deny" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, false)}>
                    <X size={13} aria-hidden="true" />拒绝
                </button>
                <button className="approval-gate-approve" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, true)}>
                    <Check size={13} aria-hidden="true" />批准执行
                </button>
            </div>
        </div>
    </section>
}
