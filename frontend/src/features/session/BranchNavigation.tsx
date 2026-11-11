import { useState } from 'react'
import { GitBranch, GitFork, Trash2, X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import type { Branch, getEntryTree, listBranches } from '../../api'
import { readableError } from '../../shared/errors'
import './branches.css'

type BranchList = Awaited<ReturnType<typeof listBranches>>
type EntryTree = Awaited<ReturnType<typeof getEntryTree>>

export function BranchStatus({ branch, browsing, busy, onReturn, cleanupPending, onRetryCleanup }: { branch: Branch; browsing: boolean; busy: boolean; onReturn: () => void; cleanupPending: boolean; onRetryCleanup: () => void }) {
    return <div className="session-branch-status">
        <GitBranch size={14} aria-hidden="true" /><span>{branch.name}</span>
        {cleanupPending && <button type="button" className="quiet-button" disabled={busy} onClick={onRetryCleanup}>重试目录清理</button>}
        {browsing && <><span className="session-branch-hint">历史浏览，仅执行结束位置允许分叉</span><button type="button" className="quiet-button" disabled={busy} onClick={onReturn}>返回最新位置</button></>}
    </div>
}

type DialogProps = {
    kind: 'branches' | 'tree'
    branches: BranchList
    tree: EntryTree
    currentBranchId: string
    onSwitch: (name: string) => Promise<void>
    onBrowse: (entryId: string) => Promise<void>
    onDelete: (branchId: string) => Promise<void>
    onClose: () => void
}

/** 选择节点只改变展示；模型 Context 在真正创建分支时恢复。 */
export function BranchDialog({ kind, branches, tree, currentBranchId, onSwitch, onBrowse, onDelete, onClose }: DialogProps) {
    const [pending, setPending] = useState(false)
    const [error, setError] = useState('')
    const [deleteTarget, setDeleteTarget] = useState('')
    const select = async (apply: () => Promise<void>) => {
        if (pending) return
        setPending(true)
        setError('')
        try { await apply() } catch (reason) { setError(readableError(reason)) } finally { setPending(false) }
    }
    const depths = new Map<string, number>()
    const nodes = tree.map((node) => {
        const depth = node.parentId ? (depths.get(node.parentId) ?? 0) + 1 : 0
        depths.set(node.id, depth)
        return { ...node, depth }
    })
    const close = () => { if (!pending) { onClose(); document.getElementById('agent-input')?.focus() } }
    return <Overlay labelledBy="session-branch-dialog-title" onClose={close}>
        <section className="session-branch-dialog" aria-busy={pending}>
            <header>
                <h2 id="session-branch-dialog-title">{kind === 'branches' ? '切换分支' : '条目树'}</h2>
                <button className="icon-button" type="button" aria-label="关闭" disabled={pending} onClick={close}><X size={18} /></button>
            </header>
            <p className="session-branch-description">{kind === 'branches' ? '切换后回到该分支的最新位置。' : '任意条目均可浏览；只有主会话执行结束位置允许分叉，其他条目仅可查看。分支不隔离项目文件。'}</p>
            {error && <p className="inline-error" role="alert">{error}</p>}
            {deleteTarget && <div className="session-branch-confirm">
                <p>确定删除分支「{branches.find(({ branch }) => branch.id === deleteTarget)?.branch.name}」及其独有历史？共享祖先保留，此操作不可恢复。</p>
                <button type="button" className="modal-primary modal-danger" disabled={pending} onClick={() => void select(() => onDelete(deleteTarget))}>确认删除</button>
                <button type="button" className="quiet-button" disabled={pending} onClick={() => setDeleteTarget('')}>取消</button>
            </div>}
            <div className="session-branch-options">
                {kind === 'branches' ? branches.map(({ branch, summary }) => (
                    <div className="session-branch-row" key={branch.id}>
                        <button type="button" className="session-branch-option" disabled={pending || Boolean(deleteTarget)} onClick={() => void select(() => onSwitch(branch.name))}>
                            <GitBranch size={16} aria-hidden="true" />
                            <span><strong>{branch.name}{branch.id === currentBranchId ? '（当前）' : ''}</strong><small>{summary || '暂无消息'}</small><small>{new Date(branch.updated_at).toLocaleString()}</small></span>
                        </button>
                        <button type="button" className="icon-button session-branch-delete" disabled={pending || Boolean(deleteTarget) || !branch.source_branch_id || branches.some((item) => item.branch.source_branch_id === branch.id)} aria-label={`删除分支：${branch.name}`} title={!branch.source_branch_id ? '根分支不可删除' : branches.some((item) => item.branch.source_branch_id === branch.id) ? '请先删除子分支' : '删除分支及其独有历史'} onClick={() => setDeleteTarget(branch.id)}><Trash2 size={16} aria-hidden="true" /></button>
                    </div>
                )) : <>
                    <button type="button" className="session-branch-option" disabled={pending} onClick={() => void select(() => onBrowse(''))}><GitFork size={16} aria-hidden="true" /><span>条目起点</span></button>
                    {nodes.map((node) => (
                        <button type="button" className="session-branch-option session-tree-node" disabled={pending} key={node.id} style={{ paddingInlineStart: `${12 + Math.min(node.depth, 12) * 14}px` }} onClick={() => void select(() => onBrowse(node.id))}>
                            <GitFork size={14} aria-hidden="true" />
                            <span><strong>{node.summary}</strong><small>{branches.filter(({ branch }) => branch.head_entry_id === node.id).map(({ branch }) => branch.name).join(' · ') || node.id}{node.safe ? ' · 可创建分支' : ' · 仅可查看'}</small></span>
                        </button>
                    ))}
                </>}
            </div>
        </section>
    </Overlay>
}
