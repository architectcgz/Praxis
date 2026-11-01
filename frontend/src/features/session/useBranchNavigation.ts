import { useCallback, useRef, useState } from 'react'
import { createBranch, getHistoryAt, getHistoryTree, listBranches, switchBranch, type Entry, type SessionSnapshot } from '../../api'

type BranchList = Awaited<ReturnType<typeof listBranches>>
type HistoryTree = Awaited<ReturnType<typeof getHistoryTree>>
type Options = {
    session: SessionSnapshot | null
    applySession: (session: SessionSnapshot) => Promise<void>
}

/** 浏览与分支选择只属于页面，写入绑定在发送消息时创建。 */
export function useBranchNavigation({ session, applySession }: Options) {
    const scope = session ? JSON.stringify([session.id, session.branch.id]) : ''
    const [dialog, setDialog] = useState<{ scope: string; kind: 'branches' | 'tree' } | null>(null)
    const [branches, setBranches] = useState<BranchList>([])
    const [tree, setTree] = useState<HistoryTree>([])
    const [preview, setPreview] = useState<{ scope: string; entryId: string; entries: Entry[] } | null>(null)
    const branchRequest = useRef<{ key: string; id: string } | null>(null)
    const browsing = preview?.scope === scope ? preview : null

    const create = useCallback(async (name = '') => {
        if (!session) throw new Error('当前会话不可用。')
        const entryId = browsing?.entryId ?? session.branch.head_entry_id
        const key = JSON.stringify([scope, entryId, name])
        if (branchRequest.current?.key !== key) branchRequest.current = { key, id: crypto.randomUUID() }
        const next = await createBranch(session, entryId, name, branchRequest.current.id)
        await applySession(next)
        branchRequest.current = null
        setPreview(null)
        setDialog(null)
        return next
    }, [session, scope, browsing, applySession])

    const switchTo = useCallback(async (name: string) => {
        if (!session) throw new Error('当前会话不可用。')
        const next = await switchBranch(session, name)
        await applySession(next)
        setPreview(null)
        setDialog(null)
    }, [session, applySession])

    const show = useCallback(async (kind: 'branches' | 'tree') => {
        if (!session) throw new Error('当前会话不可用。')
        if (kind === 'branches') setBranches(await listBranches(session.id, session.bindingId))
        else {
            const [branches, tree] = await Promise.all([listBranches(session.id, session.bindingId), getHistoryTree(session.id)])
            setBranches(branches)
            setTree(tree)
        }
        setDialog({ scope, kind })
    }, [session, scope])

    const browse = useCallback(async (entryId: string) => {
        if (!session) throw new Error('当前会话不可用。')
        const entries = await getHistoryAt(session.id, entryId)
        setPreview({ scope, entryId, entries })
        setDialog(null)
    }, [session, scope])

    const prepareSubmission = useCallback(async () => {
        if (!session) throw new Error('当前会话不可用。')
        return browsing && browsing.entryId !== session.branch.head_entry_id ? create() : session
    }, [session, browsing, create])

    return {
        branchDialog: dialog?.scope === scope ? dialog?.kind : undefined,
        branches, tree, browsing,
        createBranch: create, switchBranch: switchTo, showBranches: show, browseHistory: browse, prepareSubmission,
        closeBranchDialog: () => setDialog(null),
        returnToHead: () => setPreview(null),
    }
}
