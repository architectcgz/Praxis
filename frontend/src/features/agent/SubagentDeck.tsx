import { useEffect, useEffectEvent, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { Activity, AlertCircle, ArrowLeft, Bot, Check, LayoutGrid, LoaderCircle, ShieldAlert, Square, X } from 'lucide-react'
import { activeRunId, type AgentView, type HistoryMessage, type ToolApproval } from '../../api'
import type { AgentOutputSource } from '../session/sessionStore'
import { RunDuration } from '../timing/DurationLabel'
import { AgentConversation } from './AgentConversation'
import type { RequestActivities } from './history/entries'
import { formatCompactCount, formatUsageDetails, type UsageTotals } from './modelUsage'
import type { AgentDirectory } from './output/HandoffViews'
import { OutputStatus } from './output/OutputStatus'
import type { ToolStatus } from './output/toolPresentation'
import { ToolApprovalsProvider, useToolApprovals } from './ToolApprovals'
import type { StreamingOutput } from './types'

type OverviewFilter = 'all' | 'active' | 'issue' | 'done'

export type SubagentDeckProps = {
    subagents: AgentView[]
    agentDirectory?: AgentDirectory
    activeView: 'overview' | string
    inspectedSubagent: AgentView | null
    inspectedEntries: HistoryMessage[]
    inspectedRequests: RequestActivities
    pendingApprovals: ToolApproval[]
    inspectedLoading: boolean
    inspectedHistoryLoading: boolean
    inspectedHasEarlier: boolean
    inspectedEntriesError: string
    agentEntries: Record<string, HistoryMessage[]>
    agentUsages: Record<string, UsageTotals>
    outputStore: AgentOutputSource
    queuedCount: number
    queuePaused: boolean
    busy: boolean
    modalOpen: boolean
    onSelectOverview: () => void
    onSelectSubagent: (id: string) => void
    onCloseDeck: () => void
    onStopSubagent: () => void
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
}

function subagentTone(agent: AgentView): { kind: 'warn' | 'live' | 'fail' | 'done'; status: ToolStatus; label: string } {
    if (agent.state === 'waiting') return { kind: 'warn', status: 'blocked', label: '待审批' }
    if (agent.state === 'executing') return { kind: 'live', status: 'running', label: '执行中' }
    if (agent.state === 'failed') return { kind: 'fail', status: 'failed', label: '失败' }
    if (agent.state === 'interrupted') return { kind: 'fail', status: 'cancelled', label: '已中断' }
    if (agent.runOutcome === 'yielded') return { kind: 'done', status: 'completed', label: '已交付' }
    if (agent.runOutcome === 'completed') return { kind: 'done', status: 'completed', label: '已完成' }
    if (agent.state === 'unknown') return { kind: 'done', status: 'info', label: '仅历史' }
    return { kind: 'done', status: 'queued', label: '空闲' }
}

function priorityRank(agent: AgentView): number {
    if (agent.state === 'waiting') return 0
    if (agent.state === 'executing') return 1
    if (agent.state === 'failed' || agent.state === 'interrupted') return 2
    return 3
}

function parseTimeMs(value?: string): number | null {
    if (!value) return null
    const parsed = Date.parse(value)
    return Number.isNaN(parsed) ? null : parsed
}

function formatSeconds(sec: number): string {
    const safe = Math.max(0, Math.round(sec))
    if (safe < 60) return `${safe}s`
    const minutes = Math.floor(safe / 60)
    const remain = safe % 60
    return remain > 0 ? `${minutes}m ${remain}s` : `${minutes}m`
}

function summarizeSubagentActivity(agent: AgentView, entries?: HistoryMessage[], streaming?: StreamingOutput): string {
    if (streaming) {
        const lastTurn = streaming.turns.at(-1)
        if (lastTurn) {
            for (let i = lastTurn.events.length - 1; i >= 0; i -= 1) {
                const event = lastTurn.events[i]
                if (event.kind === 'tool_call') return `调用工具 ${event.data.name}`
                if (event.kind === 'text_delta' && event.data.text.trim()) {
                    return event.data.text.trim().slice(0, 88)
                }
            }
        }
        if (streaming.error) return streaming.error
    }
    if (agent.runFailure) return agent.runFailure
    if (entries && entries.length > 0) {
        const textOf = (message?: HistoryMessage) => message?.content
            .filter((block) => block.kind === 'text')
            .map((block) => block.text || '')
            .join('')
            .trim() || ''
        const reply = entries.findLast((item) => item.role === 'assistant' && textOf(item))
        if (reply) return textOf(reply).replace(/\s+/g, ' ').slice(0, 96)
        const prompt = entries.find((item) => item.role === 'user' && textOf(item))
        if (prompt) return textOf(prompt).replace(/\s+/g, ' ').slice(0, 96)
    }
    if (agent.state === 'waiting') return '等待工具审批确认'
    if (agent.state === 'executing') return '正在隔离上下文中执行委派任务'
    if (agent.runOutcome === 'yielded') return '已向主代理交付结果，点击查看完整历史'
    if (agent.runOutcome === 'completed') return '已完成执行，点击查看完整历史'
    return '点击查看独立上下文记录'
}

/** 右侧 M3 一体化浮岛：永久固定第 0 位「总览」Tab + 横滑子代理标签轨 + O2 微型并行时间线总览 / 单代理详情。 */
export function SubagentDeck({
    subagents,
    agentDirectory,
    activeView,
    inspectedSubagent,
    inspectedEntries,
    inspectedRequests,
    pendingApprovals,
    inspectedLoading,
    inspectedHistoryLoading,
    inspectedHasEarlier,
    inspectedEntriesError,
    agentEntries,
    agentUsages,
    outputStore,
    queuedCount,
    queuePaused,
    busy,
    modalOpen,
    onSelectOverview,
    onSelectSubagent,
    onCloseDeck,
    onStopSubagent,
    onLoadEarlier,
    onRetryHistory,
}: SubagentDeckProps) {
    const { streamingOutputs } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    const [ovFilter, setOvFilter] = useState<OverviewFilter>('all')
    const [rosterOpen, setRosterOpen] = useState(false)
    const [nowMs, setNowMs] = useState(() => Date.now())
    const railRef = useRef<HTMLDivElement>(null)

    const sortedSubagents = useMemo(() => [...subagents].sort((left, right) => {
        const rankDiff = priorityRank(left) - priorityRank(right)
        if (rankDiff !== 0) return rankDiff
        const leftAt = left.runStartedAt ?? left.createdAt ?? ''
        const rightAt = right.runStartedAt ?? right.createdAt ?? ''
        return leftAt.localeCompare(rightAt)
    }), [subagents])

    const hasRunning = useMemo(
        () => sortedSubagents.some((item) => item.state === 'executing' || item.state === 'waiting'),
        [sortedSubagents],
    )

    useEffect(() => {
        if (!hasRunning) return
        const timer = window.setInterval(() => setNowMs(Date.now()), 1000)
        return () => window.clearInterval(timer)
    }, [hasRunning])

    useEffect(() => {
        const rail = railRef.current
        if (!rail) return
        const handleWheel = (event: WheelEvent) => {
            if (Math.abs(event.deltaY) > Math.abs(event.deltaX) && rail.scrollWidth > rail.clientWidth) {
                rail.scrollLeft += event.deltaY
                event.preventDefault()
            }
        }
        rail.addEventListener('wheel', handleWheel, { passive: false })
        return () => rail.removeEventListener('wheel', handleWheel)
    }, [])

    const handleEscape = useEffectEvent((event: KeyboardEvent) => {
        if (event.key !== 'Escape' || modalOpen) return
        if (rosterOpen) {
            setRosterOpen(false)
            return
        }
        if (activeView !== 'overview') {
            onSelectOverview()
        }
    })

    useEffect(() => {
        window.addEventListener('keydown', handleEscape)
        return () => window.removeEventListener('keydown', handleEscape)
    }, [])

    const counts = useMemo(() => {
        let done = 0
        let live = 0
        let warn = 0
        let fail = 0
        for (const item of sortedSubagents) {
            const tone = subagentTone(item).kind
            if (tone === 'warn') warn += 1
            else if (tone === 'live') live += 1
            else if (tone === 'fail') fail += 1
            else done += 1
        }
        return { total: sortedSubagents.length, done, live, warn, fail }
    }, [sortedSubagents])

    const timelineBounds = useMemo(() => {
        let minStart = Number.POSITIVE_INFINITY
        let maxEnd = 0
        for (const item of sortedSubagents) {
            const start = parseTimeMs(item.runStartedAt ?? item.createdAt)
            const end = parseTimeMs(item.runEndedAt) ?? (item.state === 'executing' || item.state === 'waiting' ? nowMs : start)
            if (start !== null && start < minStart) minStart = start
            if (end !== null && end > maxEnd) maxEnd = end
        }
        if (!Number.isFinite(minStart)) {
            return { originMs: nowMs, maxSpanSec: 30, nowSec: 0 }
        }
        const effectiveNow = hasRunning ? Math.max(nowMs, maxEnd) : Math.max(maxEnd, minStart + 1000)
        const nowSec = Math.max(1, (effectiveNow - minStart) / 1000)
        const maxSpanSec = Math.max(10, Math.ceil(nowSec * 1.08))
        return { originMs: minStart, maxSpanSec, nowSec }
    }, [sortedSubagents, hasRunning, nowMs])

    const filteredSubagents = useMemo(() => sortedSubagents.filter((item) => {
        const tone = subagentTone(item).kind
        if (ovFilter === 'active') return tone === 'live' || tone === 'warn'
        if (ovFilter === 'issue') return tone === 'warn' || tone === 'fail'
        if (ovFilter === 'done') return tone === 'done'
        return true
    }), [sortedSubagents, ovFilter])

    const activeGroup = useMemo(() => filteredSubagents.filter((item) => subagentTone(item).kind !== 'done'), [filteredSubagents])
    const doneGroup = useMemo(() => filteredSubagents.filter((item) => subagentTone(item).kind === 'done'), [filteredSubagents])
    const blockedAgents = useMemo(() => sortedSubagents.filter((item) => item.state === 'waiting' || item.state === 'failed'), [sortedSubagents])
    const totalToolCalls = useMemo(() => sortedSubagents.reduce((sum, item) => sum + (item.runToolCalls ?? 0), 0), [sortedSubagents])

    const showingDetail = activeView !== 'overview' && inspectedSubagent !== null
    const inspectedTone = inspectedSubagent ? subagentTone(inspectedSubagent) : null
    const inspectedUsage = inspectedSubagent ? agentUsages[inspectedSubagent.ID] : undefined
    const inspectedActive = Boolean(activeRunId(inspectedSubagent))

    const openAgent = (id: string) => {
        setRosterOpen(false)
        onSelectSubagent(id)
    }

    const totalDenom = Math.max(1, counts.total)
    const donePct = Math.round((counts.done / totalDenom) * 100)
    const ticks = [0, Math.round(timelineBounds.maxSpanSec * 0.33), Math.round(timelineBounds.maxSpanSec * 0.66), timelineBounds.maxSpanSec]
    const nowPct = Math.min(98, (timelineBounds.nowSec / timelineBounds.maxSpanSec) * 100)

    const renderTimelineRow = (agent: AgentView, compact: boolean) => {
        const tone = subagentTone(agent)
        const usage = agentUsages[agent.ID]
        const startMs = parseTimeMs(agent.runStartedAt ?? agent.createdAt) ?? timelineBounds.originMs
        const endMs = parseTimeMs(agent.runEndedAt) ?? (agent.state === 'executing' || agent.state === 'waiting' ? Math.max(nowMs, startMs) : startMs + 1000)
        const startSec = Math.max(0, (startMs - timelineBounds.originMs) / 1000)
        const endSec = Math.max(startSec + 0.5, (endMs - timelineBounds.originMs) / 1000)
        const leftPct = Math.min(94, (startSec / timelineBounds.maxSpanSec) * 100)
        const widthPct = Math.min(100 - leftPct, Math.max(4, ((endSec - startSec) / timelineBounds.maxSpanSec) * 100))
        const durSec = Math.max(1, endSec - startSec)
        const activity = summarizeSubagentActivity(agent, agentEntries[agent.ID], streamingOutputs[agent.ID])
        const usageTooltip = usage?.hasUsage ? formatUsageDetails(usage, `${agent.name} 独立用量`) : ''

        return (
            <button
                className={`deck-tl-row ${compact ? 'is-compact' : ''}`}
                key={agent.ID}
                type="button"
                onClick={() => openAgent(agent.ID)}
            >
                <div className="deck-tl-info">
                    <div className="deck-tl-info-top">
                        <span className={`deck-dot is-${tone.kind}`} aria-hidden="true" />
                        <span className="deck-tl-name">{agent.name}</span>
                        <span className={`deck-tag is-${tone.kind}`}>{tone.label}</span>
                    </div>
                    <div className="deck-tl-sub" title={activity}>{activity}</div>
                    <div className="deck-tl-meta" title={usageTooltip || undefined}>
                        {!compact && (agent.runIterations !== undefined || agent.runToolCalls !== undefined) && (
                            <>
                                <span>{agent.runIterations ?? 0} 轮 · {agent.runToolCalls ?? 0} 工具</span>
                                {usage?.hasUsage && <span aria-hidden="true">·</span>}
                            </>
                        )}
                        {usage?.hasUsage ? (
                            <>
                                {usage.hasInput && <span>输入 {formatCompactCount(usage.inputTokens)}</span>}
                                {usage.hasOutput && <span>输出 {formatCompactCount(usage.outputTokens)}</span>}
                                {usage.cacheRate !== null && <span className="deck-tl-cache">缓存 {usage.cacheRate}%</span>}
                            </>
                        ) : (
                            compact && agent.runToolCalls !== undefined && <span>{agent.runToolCalls} 工具</span>
                        )}
                    </div>
                </div>
                <div className="deck-tl-lane-wrap" aria-hidden="true">
                    <div className="deck-tl-lane-bg">
                        <span className="deck-tl-grid-tick" style={{ left: '33%' }} />
                        <span className="deck-tl-grid-tick" style={{ left: '66%' }} />
                        <span className="deck-tl-now-line" style={{ left: `${nowPct}%` }} />
                        <span className={`deck-tl-bar is-${tone.kind}`} style={{ left: `${leftPct}%`, width: `${widthPct}%` }} />
                    </div>
                    <span className="deck-tl-dur">{formatSeconds(durSec)}</span>
                </div>
            </button>
        )
    }

    return (
        <aside className="workbench-sheet right-sheet subagent-deck" aria-label="子代理协作总览与详情">
            {/* M3 一体化顶轨：永久固定第 0 位「总览」 + 横滑标签轨 + 「全部 N」 */}
            <div className="deck-top-rail">
                <button
                    className={`deck-overview-tab ${!showingDetail ? 'is-active' : ''}`}
                    type="button"
                    aria-current={!showingDetail ? 'page' : undefined}
                    onClick={() => { setRosterOpen(false); onSelectOverview() }}
                >
                    <Activity size={13} strokeWidth={2} aria-hidden="true" />
                    <span>总览</span>
                    <span className="deck-tab-count">{counts.done}/{counts.total}</span>
                </button>

                <span className="deck-rail-divider" aria-hidden="true" />

                <div className="deck-tabs-rail" ref={railRef} role="tablist" aria-label="子代理切换轨">
                    {sortedSubagents.map((agent) => {
                        const tone = subagentTone(agent)
                        const isSelected = showingDetail && inspectedSubagent?.ID === agent.ID
                        return (
                            <button
                                className={`deck-agent-tab ${isSelected ? 'is-active' : ''}`}
                                key={agent.ID}
                                type="button"
                                role="tab"
                                aria-selected={isSelected}
                                onClick={() => openAgent(agent.ID)}
                            >
                                <span className={`deck-dot is-${tone.kind}`} aria-hidden="true" />
                                <span className="deck-agent-tab-name">{agent.name}</span>
                                {(tone.kind === 'warn' || tone.kind === 'fail') && (
                                    <span className={`deck-agent-tab-flag is-${tone.kind}`}>{tone.label}</span>
                                )}
                            </button>
                        )
                    })}
                </div>

                <button
                    className={`deck-roster-trigger ${rosterOpen ? 'is-open' : ''}`}
                    type="button"
                    aria-expanded={rosterOpen}
                    aria-label={`展开全部 ${counts.total} 个子代理`}
                    onClick={() => setRosterOpen((open) => !open)}
                >
                    <LayoutGrid size={13} strokeWidth={1.9} aria-hidden="true" />
                    <span>全部 {counts.total}</span>
                </button>
                <button
                    className="icon-button deck-close-button"
                    type="button"
                    title="收起右侧子代理面板"
                    aria-label="收起右侧子代理面板"
                    onClick={() => { setRosterOpen(false); onCloseDeck() }}
                >
                    <X size={14} strokeWidth={1.9} aria-hidden="true" />
                </button>
            </div>

            {rosterOpen && (
                <div className="deck-roster-popover" role="dialog" aria-label="全部子代理快速直达">
                    <div className="deck-roster-head">
                        <strong>全部子代理 ({counts.total})</strong>
                        <button
                            className="icon-button"
                            type="button"
                            aria-label="关闭子代理列表"
                            onClick={() => setRosterOpen(false)}
                        >
                            <X size={14} strokeWidth={1.9} aria-hidden="true" />
                        </button>
                    </div>
                    <div className="deck-roster-grid">
                        {sortedSubagents.map((agent) => {
                            const tone = subagentTone(agent)
                            const usage = agentUsages[agent.ID]
                            const isSelected = showingDetail && inspectedSubagent?.ID === agent.ID
                            return (
                                <button
                                    className={`deck-roster-card ${isSelected ? 'is-active' : ''}`}
                                    key={agent.ID}
                                    type="button"
                                    onClick={() => openAgent(agent.ID)}
                                >
                                    <div className="deck-roster-card-top">
                                        <span className="deck-roster-card-name">
                                            <span className={`deck-dot is-${tone.kind}`} aria-hidden="true" />
                                            <span>{agent.name}</span>
                                        </span>
                                        <span className={`deck-tag is-${tone.kind}`}>{tone.label}</span>
                                    </div>
                                    <div className="deck-roster-card-sub">
                                        {summarizeSubagentActivity(agent, agentEntries[agent.ID], streamingOutputs[agent.ID])}
                                    </div>
                                    {usage?.hasUsage && (
                                        <div className="deck-roster-card-usage">
                                            输入 {formatCompactCount(usage.inputTokens)} · 输出 {formatCompactCount(usage.outputTokens)}
                                            {usage.cacheRate !== null ? ` · 缓存 ${usage.cacheRate}%` : ''}
                                        </div>
                                    )}
                                </button>
                            )
                        })}
                    </div>
                </div>
            )}

            {showingDetail && inspectedSubagent && inspectedTone ? (
                <div className="deck-detail-shell">
                    <div className="deck-detail-header">
                        <div className="deck-detail-row">
                            <div className="deck-detail-identity">
                                <button className="quiet-button deck-back-button" type="button" onClick={onSelectOverview}>
                                    <ArrowLeft size={13} strokeWidth={1.9} aria-hidden="true" />
                                    <span>总览</span>
                                </button>
                                <span className="deck-rail-divider" aria-hidden="true" />
                                <span className={`deck-dot is-${inspectedTone.kind}`} aria-hidden="true" />
                                <strong className="deck-detail-name">{inspectedSubagent.name}</strong>
                                <OutputStatus status={inspectedTone.status} label={inspectedTone.label} />
                            </div>
                            <div className="deck-detail-actions">
                                <span className="deck-detail-metrics">
                                    {inspectedSubagent.runIterations !== undefined && `${inspectedSubagent.runIterations} 轮 · `}
                                    {inspectedSubagent.runToolCalls !== undefined && `${inspectedSubagent.runToolCalls} 工具 · `}
                                    <RunDuration startedAt={inspectedSubagent.runStartedAt} endedAt={inspectedSubagent.runEndedAt} />
                                </span>
                                {inspectedActive && (
                                    <button
                                        className="subagent-stop-button"
                                        type="button"
                                        disabled={busy}
                                        onClick={onStopSubagent}
                                    >
                                        <Square size={11} strokeWidth={2} aria-hidden="true" />
                                        <span>停止</span>
                                    </button>
                                )}
                            </div>
                        </div>
                        {inspectedUsage?.hasUsage && (
                            <div
                                className="usage-inline deck-detail-usage"
                                title={formatUsageDetails(inspectedUsage, `${inspectedSubagent.name} 独立用量`)}
                            >
                                <span>独立用量 <strong>{formatCompactCount(inspectedUsage.totalTokens)}</strong></span>
                                {inspectedUsage.hasInput && <span>输入 {formatCompactCount(inspectedUsage.inputTokens)}</span>}
                                {inspectedUsage.hasOutput && <span>输出 {formatCompactCount(inspectedUsage.outputTokens)}</span>}
                                {inspectedUsage.cacheRate !== null && <span className="usage-cache-rate">缓存 {inspectedUsage.cacheRate}%</span>}
                                {(inspectedUsage.hasCacheRead || inspectedUsage.hasCacheCreation) && (
                                    <span className="usage-cache-detail">
                                        (缓存读 {formatCompactCount(inspectedUsage.cacheReadInputTokens)} · 缓存写 {formatCompactCount(inspectedUsage.cacheCreationInputTokens)})
                                    </span>
                                )}
                            </div>
                        )}
                    </div>

                    <div className="deck-detail-body">
                        {inspectedLoading ? (
                            <div className="agent-loading" aria-busy="true">
                                <LoaderCircle size={18} className="is-spinning" aria-hidden="true" />
                                <span>加载子代理历史中</span>
                            </div>
                        ) : (
                            <AgentConversation
                                key={`${inspectedSubagent.SessionID}:${inspectedSubagent.ID}`}
                                submissionRevision={0}
                                agent={inspectedSubagent}
                                agentDirectory={agentDirectory}
                                entries={inspectedEntries}
                                requests={inspectedRequests}
                                pendingApprovals={pendingApprovals}
                                onOpenSubagent={openAgent}
                                streamingOutput={streamingOutputs[inspectedSubagent.ID] || null}
                                entriesError={inspectedEntriesError}
                                hasEarlier={inspectedHasEarlier}
                                historyLoading={inspectedHistoryLoading}
                                onLoadEarlier={onLoadEarlier}
                                onRetryHistory={onRetryHistory}
                                busy={busy}
                                composer={null}
                            />
                        )}
                    </div>

                    <footer className="workbench-sheet-footer deck-detail-footer">
                        <span className="deck-detail-footer-copy">
                            <Bot size={13} strokeWidth={1.8} aria-hidden="true" />
                            <span>只读委派上下文 · 按 Esc 返回总览</span>
                        </span>
                        <span className="deck-footer-metrics">
                            {inspectedSubagent.runToolCalls !== undefined
                                ? `${inspectedSubagent.runToolCalls} 次工具调用`
                                : inspectedTone.label}
                        </span>
                    </footer>
                </div>
            ) : (
                <>
                    {/* O2 总览视图：多段进度带 -> 阻断快处 -> 微型并行时间线 -> 轻态底部摘要 */}
                    <div className="deck-overview-scroll">
                        <section className="deck-ribbon-card" aria-label="子代理总体进度">
                            <div className="deck-ribbon-top">
                                <div className="deck-ratio">
                                    <strong>{donePct}%</strong>
                                    <span>已完成 {counts.done} / {counts.total} 个子代理</span>
                                </div>
                                <span className="deck-elapsed">总经过 {formatSeconds(timelineBounds.nowSec)}</span>
                            </div>

                            <div className="deck-seg-track" aria-hidden="true">
                                {counts.done > 0 && <span className="is-done" style={{ width: `${(counts.done / totalDenom) * 100}%` }} />}
                                {counts.live > 0 && <span className="is-live" style={{ width: `${(counts.live / totalDenom) * 100}%` }} />}
                                {counts.warn > 0 && <span className="is-warn" style={{ width: `${(counts.warn / totalDenom) * 100}%` }} />}
                                {counts.fail > 0 && <span className="is-fail" style={{ width: `${(counts.fail / totalDenom) * 100}%` }} />}
                            </div>

                            <div className="deck-stats-row">
                                <div className="deck-legend">
                                    <span><i className="deck-dot is-done" />已交付 {counts.done}</span>
                                    <span><i className="deck-dot is-live" />执行中 {counts.live}</span>
                                    <span><i className="deck-dot is-warn" />待审批 {counts.warn}</span>
                                    {counts.fail > 0 && <span><i className="deck-dot is-fail" />失败 {counts.fail}</span>}
                                </div>
                                <div className="deck-filters" role="group" aria-label="按状态筛选子代理">
                                    {([
                                        ['all', '全部'],
                                        ['active', '活跃'],
                                        ['issue', '需处理'],
                                        ['done', '已交付'],
                                    ] as const).map(([key, label]) => (
                                        <button
                                            className={`deck-filter-chip ${ovFilter === key ? 'is-active' : ''}`}
                                            key={key}
                                            type="button"
                                            onClick={() => setOvFilter(key)}
                                        >
                                            {label}
                                        </button>
                                    ))}
                                </div>
                            </div>
                        </section>

                        {blockedAgents.length > 0 && (
                            <section className="deck-alert-stack" aria-label="需要处理的子代理">
                                {blockedAgents.map((agent) => {
                                    const runId = activeRunId(agent)
                                    if (agent.state === 'waiting' && runId) {
                                        const agentApprovals = pendingApprovals.filter((item) => item.agentId === agent.ID && item.runId === runId)
                                        return (
                                            <ToolApprovalsProvider key={`ov-approval-${agent.ID}-${runId}`} pendingApprovals={agentApprovals}>
                                                <SubagentInlineApproval agent={agent} onInspect={() => openAgent(agent.ID)} />
                                            </ToolApprovalsProvider>
                                        )
                                    }
                                    return (
                                        <div className="deck-alert-card is-fail" key={`ov-fail-${agent.ID}`}>
                                            <div className="deck-alert-copy">
                                                <strong>
                                                    <AlertCircle size={13} strokeWidth={2} aria-hidden="true" />
                                                    <span>{agent.name} 执行失败</span>
                                                </strong>
                                                <p>{agent.runFailure || '运行异常终止，点击查看完整日志与错误上下文。'}</p>
                                            </div>
                                            <button className="output-button" type="button" onClick={() => openAgent(agent.ID)}>查看排错</button>
                                        </div>
                                    )
                                })}
                            </section>
                        )}

                        <section className="deck-tl-card" aria-label="子代理并行时间线">
                            <div className="deck-tl-ruler">
                                <span>子代理与实时动作（点击查看详情）</span>
                                <div className="deck-tl-scale" aria-hidden="true">
                                    {ticks.map((tick, index) => <span key={`${tick}-${index}`}>{formatSeconds(tick)}</span>)}
                                </div>
                            </div>

                            {activeGroup.length > 0 && (
                                <>
                                    <div className="deck-tl-group">
                                        <span>活跃与待处理 ({activeGroup.length})</span>
                                        <span>NOW = {formatSeconds(timelineBounds.nowSec)}</span>
                                    </div>
                                    {activeGroup.map((agent) => renderTimelineRow(agent, false))}
                                </>
                            )}

                            {doneGroup.length > 0 && (
                                <>
                                    <div className="deck-tl-group">
                                        <span>已交付 / 已收尾 ({doneGroup.length})</span>
                                        <span>紧凑视图</span>
                                    </div>
                                    {doneGroup.map((agent) => renderTimelineRow(agent, true))}
                                </>
                            )}

                            {filteredSubagents.length === 0 && (
                                <p className="deck-tl-empty">当前筛选条件下没有子代理。</p>
                            )}
                        </section>
                    </div>

                    <footer className="workbench-sheet-footer deck-overview-footer">
                        <span>
                            主队列：{queuedCount > 0 ? (queuePaused ? `已暂停（待执行 ${queuedCount} 项）` : `待执行 ${queuedCount} 项`) : '空闲'}
                        </span>
                        <span className="deck-footer-metrics">子代理累计 {totalToolCalls} 次工具调用</span>
                    </footer>
                </>
            )}
        </aside>
    )
}

/** 在总览面板内直接就地批准或拒绝子代理的待审批调用。 */
function SubagentInlineApproval({ agent, onInspect }: { agent: AgentView; onInspect: () => void }) {
    const approvals = useToolApprovals()
    const pending = approvals?.pendingApprovals[0]
    const disabled = !approvals || approvals.busy !== ''
    const summary = pending?.name === 'bash' ? pending.arguments.command : pending?.arguments.path || pending?.name

    return (
        <div className="deck-alert-card is-warn">
            <div className="deck-alert-copy">
                <strong>
                    <ShieldAlert size={13} strokeWidth={2} aria-hidden="true" />
                    <span>{agent.name} 等待审批</span>
                    {approvals && approvals.pendingApprovals.length > 1 && (
                        <span className="deck-alert-count">1 / {approvals.pendingApprovals.length}</span>
                    )}
                </strong>
                <p title={summary || undefined}>
                    {pending
                        ? `${pending.name === 'bash' ? '$ ' : ''}${summary}`
                        : '等待工具审批确认'}
                </p>
            </div>
            <div className="deck-alert-actions">
                {pending && approvals ? (
                    <>
                        <button className="approval-gate-deny" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, false)}>
                            <X size={12} aria-hidden="true" />拒绝
                        </button>
                        <button className="approval-gate-approve" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, true)}>
                            <Check size={12} aria-hidden="true" />就地批准
                        </button>
                    </>
                ) : (
                    <button className="output-button" type="button" onClick={onInspect}>查看详情</button>
                )}
            </div>
        </div>
    )
}
