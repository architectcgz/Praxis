import { useCallback, useEffect, useRef, useState } from 'react'
import { createBranch, deleteBranch, getEntriesAt, getEntryTree, listBranches, selectSession, switchBranch, type SessionSnapshot } from '../../api'
import type { HistoryPage } from '../../api/agents'
import { readableError } from '../../shared/errors'

type BranchList = Awaited<ReturnType<typeof listBranches>>
type EntryTree = Awaited<ReturnType<typeof getEntryTree>>
type Options = {
    session: SessionSnapshot | null
    applySession: (session: SessionSnapshot) => Promise<void>
    setError: (message: string) => void
}

/** 树只在对话框打开期间保留；历史浏览与写入分支选择是不同操作。 */
export function useBranchNavigation({ session, applySession, setError }: Options) {
    const scope = session ? JSON.stringify([session.id, session.branch.id]) : ''
    const [dialog, setDialog] = useState<{ scope: string; kind: 'branches' | 'tree' } | null>(null)
    const [branches, setBranches] = useState<BranchList>([])
    const [tree, setTree] = useState<EntryTree>([])
    const [preview, setPreview] = useState<{ scope: string; entryId: string; page: HistoryPage; forkable: boolean } | null>(null)
    const [historyLoading, setHistoryLoading] = useState(false)
    const [cleanupTarget, setCleanupTarget] = useState('')
    const [usageRevision, setUsageRevision] = useState(0)
    const branchRequest = useRef<{ key: string; id: string } | null>(null)
    const request = useRef(0)
    const browsing = preview?.scope === scope ? preview : null
    const closeDialog = useCallback(() => { request.current++; setDialog(null); setBranches([]); setTree([]) }, [])

    useEffect(() => {
        request.current++
        setPreview(null); setDialog(null); setBranches([]); setTree([]); setHistoryLoading(false)
    }, [scope])

    const create = useCallback(async (name = '') => {
        if (!session) throw new Error('当前会话不可用。')
        if (browsing && !browsing.forkable) throw new Error('只能从主会话已结束执行的末尾创建分支。')
        request.current++
        const entryId = browsing?.entryId ?? session.branch.head_entry_id
        const key = JSON.stringify([scope, entryId, name])
        if (branchRequest.current?.key !== key) branchRequest.current = { key, id: crypto.randomUUID() }
        const next = await createBranch(session, entryId, name, branchRequest.current.id)
        await applySession(next)
        branchRequest.current = null
        setPreview(null); closeDialog()
        return next
    }, [session, scope, browsing, applySession, closeDialog])

    const switchTo = useCallback(async (name: string) => {
        if (!session) throw new Error('当前会话不可用。')
        request.current++
        const next = await switchBranch(session, name)
        await applySession(next)
        setPreview(null); closeDialog()
    }, [session, applySession, closeDialog])

    const show = useCallback(async (kind: 'branches' | 'tree') => {
        if (!session) throw new Error('当前会话不可用。')
        const current = ++request.current
        const branches = await listBranches(session.id)
        const tree = kind === 'tree' ? await getEntryTree(session.id) : []
        if (current !== request.current) return
        setBranches(branches); setTree(tree); setDialog({ scope, kind })
    }, [session, scope])

    const browse = useCallback(async (entryId: string) => {
        if (!session) throw new Error('当前会话不可用。')
        const current = ++request.current
        const forkable = tree.some((node) => node.id === entryId && node.safe)
        const page = await getEntriesAt(session.id, entryId)
        if (current !== request.current) return
        setPreview({ scope, entryId, page, forkable })
        closeDialog()
    }, [session, scope, tree, closeDialog])

    const loadEarlierPreview = useCallback(async () => {
        if (!session || !browsing?.page.hasMore || historyLoading) return
        const current = ++request.current
        setHistoryLoading(true)
        try {
            const earlier = await getEntriesAt(session.id, browsing.entryId, browsing.page.nextCursor)
            if (current === request.current) setPreview({ ...browsing, page: { ...earlier, entries: [...earlier.entries, ...browsing.page.entries] } })
        } catch (reason) { if (current === request.current) setError(readableError(reason)) }
        finally { if (current === request.current) setHistoryLoading(false) }
    }, [session, browsing, historyLoading, setError])

    const remove = useCallback(async (target: string) => {
        if (!session) throw new Error('当前会话不可用。')
        request.current++
        const result = await deleteBranch(session, target)
        setUsageRevision((value) => value + 1)
        closeDialog()
        setCleanupTarget(result.cleanupError ? target : '')
        setError(result.cleanupError ? `分支已删除，但目录清理失败：${result.cleanupError}` : '')
        if (result.branch.id !== session.branch.id) {
            // 先接纳已经提交的选择，再请求正文；读取失败不能让已删除分支重新可见。
            await applySession({ ...session, branch: result.branch, bindingId: result.bindingId, agents: [], entries: {}, tasks: [] })
            try { await applySession(await selectSession(session.id, result.branch.id)) }
            catch (reason) { setError(`分支已删除，新分支历史加载失败：${readableError(reason)}`) }
        }
    }, [session, applySession, closeDialog, setError])

    const prepareSubmission = useCallback(async () => {
        if (!session) throw new Error('当前会话不可用。')
        if (browsing && browsing.entryId !== session.branch.head_entry_id) throw new Error('当前为历史浏览，请先明确创建分支，或返回最新位置。')
        return session
    }, [session, browsing])

    return {
        branchDialog: dialog?.scope === scope ? dialog.kind : undefined,
        branches, tree, browsing: browsing ? { ...browsing, entries: browsing.page.entries } : null,
        createBranch: create, switchBranch: switchTo, showBranches: show, browseEntries: browse, prepareSubmission,
        usageRevision, deleteBranch: remove, cleanupTarget, retryCleanup: async () => { try { await remove(cleanupTarget) } catch (reason) { setError(readableError(reason)) } },
        previewHistoryLoading: historyLoading, loadEarlierPreview,
        closeBranchDialog: closeDialog,
        returnToHead: () => { request.current++; setPreview(null); setHistoryLoading(false) },
    }
}
