import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { AlertCircle, Check, LoaderCircle, ShieldAlert, X } from 'lucide-react'
import { approveToolCall, denyToolCall, listToolApprovals, subscribeAgentEvents, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'

type ApprovalState = {
    pendingApprovals: ToolApproval[]
    busy: string
    loading: boolean
    queryError: string
    decisionError: string
    reload: () => Promise<void>
    decide: (pendingApproval: ToolApproval, approve: boolean) => Promise<void>
}

const ToolApprovalsContext = createContext<ApprovalState | null>(null)

/** 会话内只查询和订阅一次审批状态，由审批闸口消费；重连后仍可审核同一个调用。 */
export function ToolApprovalsProvider({ agentId, runId, sessionId, children }: { agentId: string; runId: string; sessionId: string; children: ReactNode }) {
    const [pendingApprovals, setPendingApprovals] = useState<ToolApproval[]>([])
    const [busy, setBusy] = useState('')
    const [loading, setLoading] = useState(false)
    const [queryError, setQueryError] = useState('')
    const [decisionError, setDecisionError] = useState('')
    const requestGeneration = useRef(0)
    const reload = useCallback(async () => {
        const current = ++requestGeneration.current
        setLoading(true)
        try {
            const values = await listToolApprovals(sessionId, agentId)
            if (current === requestGeneration.current) {
                setPendingApprovals(values)
                setQueryError('')
            }
        } catch (err) {
            if (current === requestGeneration.current) setQueryError(readableError(err))
        } finally {
            if (current === requestGeneration.current) setLoading(false)
        }
    }, [agentId, sessionId])
    useEffect(() => {
        const unsubscribe = subscribeAgentEvents((event) => {
            if (event.runId === runId && event.sessionId === sessionId && ['tool_approval_required', 'tool_result', 'run_ended', 'request_canceled'].includes(event.kind)) void reload()
        })
        void reload()
        return () => { requestGeneration.current += 1; unsubscribe() }
    }, [runId, sessionId, reload])
    const decide = useCallback(async (pendingApproval: ToolApproval, approve: boolean) => {
        setBusy(pendingApproval.callId)
        setDecisionError('')
        try {
            if (approve) await approveToolCall(pendingApproval)
            else await denyToolCall(pendingApproval)
            await reload()
        } catch (err) { setDecisionError(readableError(err)) }
        finally { setBusy('') }
    }, [reload])
    const value = useMemo(() => ({ pendingApprovals, busy, loading, queryError, decisionError, reload, decide }), [pendingApprovals, busy, loading, queryError, decisionError, reload, decide])
    return <ToolApprovalsContext value={value}>{children}</ToolApprovalsContext>
}

/** 历史消息不读取当前审批状态；实时审批闸口仍由外层 Provider 维护。 */
export function ToolApprovalScope({ historical, children }: { historical: boolean; children: ReactNode }) {
    const approvals = useContext(ToolApprovalsContext)
    return <ToolApprovalsContext value={historical ? null : approvals}>{children}</ToolApprovalsContext>
}

/** 按任务与调用身份关联审核，供活动账本读取后端冻结的参数。 */
export function useToolApproval(runId: string, callId?: string) {
    const approvals = useContext(ToolApprovalsContext)
    return approvals ? { ...approvals, pendingApproval: approvals.pendingApprovals.find((pendingApproval) => pendingApproval.runId === runId && pendingApproval.callId === callId) } : null
}

/** 审批决定只在输入区上方的闸口处理，查询失败与提交失败分别展示。 */
export function ApprovalGate() {
    const approvals = useContext(ToolApprovalsContext)
    const [retried, setRetried] = useState(false)
    const pending = approvals?.pendingApprovals[0]
    if (!approvals || (!pending && !approvals.queryError && !approvals.decisionError && !retried)) return null
    const total = approvals.pendingApprovals.length
    const busy = Boolean(pending && approvals.busy === pending.callId)
    const disabled = approvals.busy !== '' || approvals.loading
    const path = pending?.arguments.path
    const summary = pending?.name === 'bash' ? pending.arguments.command : path
    return <section className="approval-gate" aria-label="当前执行的审批" aria-busy={busy || approvals.loading}>
        <div className="approval-gate-box">
            <div className="approval-gate-head">
                <ShieldAlert size={13} aria-hidden="true" />
                <strong>{pending ? '需要批准' : '当前执行的审批'}</strong>
                <span className="approval-gate-note" title={summary}>{summary}</span>
                {total > 1 && <span className="approval-gate-count">{1} / {total}</span>}
            </div>
            {pending && <dl className="approval-gate-details">
                <div><dt>工具</dt><dd>{pending.name}</dd></div>
                {path && <div><dt>操作路径</dt><dd><code>{path}</code></dd></div>}
                <div><dt>命中规则</dt><dd>{pending.ruleId === 'default' ? '项目或全局默认' : pending.ruleId}</dd></div>
                <div><dt>处理方式</dt><dd>{pending.action === 'auto' ? '模型不确定，转人工' : '必须人工确认'}</dd></div>
                {pending.riskVerdict && <div><dt>模型审核</dt><dd>{pending.riskReason || pending.riskVerdict}</dd></div>}
            </dl>}
            {approvals.queryError && <p className="approval-gate-error" role="alert">
                <AlertCircle size={13} aria-hidden="true" /><span>审批查询失败：{approvals.queryError}</span>
                <button className="quiet-button" type="button" disabled={disabled} onClick={() => { setRetried(true); void approvals.reload() }}>
                    {approvals.loading ? '正在重试…' : '重试查询'}
                </button>
            </p>}
            {approvals.decisionError && <p className="approval-gate-error" role="alert">
                <AlertCircle size={13} aria-hidden="true" /><span>审核提交失败：{approvals.decisionError}</span>
            </p>}
            {!pending && !approvals.queryError && <p role="status">{approvals.loading ? '正在读取审批…' : '当前没有待审批的操作'}</p>}
            {pending && <div className="approval-gate-foot">
                <span className="approval-gate-hint" role="status">
                    {busy ? <><LoaderCircle className="is-spinning" size={13} aria-hidden="true" />正在提交审核…</> : '仅授权本次调用'}
                </span>
                <button className="approval-gate-deny" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, false)}>
                    <X size={13} aria-hidden="true" />拒绝
                </button>
                <button className="approval-gate-approve" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, true)}>
                    <Check size={13} aria-hidden="true" />批准执行
                </button>
            </div>}
        </div>
    </section>
}
