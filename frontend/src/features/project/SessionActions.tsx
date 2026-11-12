import { useId, useLayoutEffect, useRef, useState } from 'react'
import { Ellipsis, FolderOpen, Pencil, Trash2, X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { sessionMenuPlacement } from './sessionMenuPlacement'

type SessionActionsProps = {
    title: string
    busy: boolean
    disabled: boolean
    onDelete: () => Promise<boolean>
    onRename: (title: string) => Promise<boolean>
    onRevealFile: () => Promise<boolean>
}

/** 会话行拥有操作菜单及确认弹窗；实际命令和失败展示由工作区回调负责。 */
export function SessionActions({ title, busy, disabled, onDelete, onRename, onRevealFile }: SessionActionsProps) {
    const menuID = useId()
    const [open, setOpen] = useState(false)
    const [dialog, setDialog] = useState<'delete' | 'rename' | null>(null)
    const menuRef = useRef<HTMLDivElement>(null)
    const triggerRef = useRef<HTMLButtonElement>(null)

    useLayoutEffect(() => {
        const menu = menuRef.current
        const anchor = menu?.parentElement
        const scroll = anchor?.closest<HTMLElement>('.scroll-region')
        const trigger = triggerRef.current
        if (!open || !menu || !anchor || !scroll || !trigger) return
        const closeMenu = (restoreFocus = false) => {
            if (restoreFocus) trigger.focus({ preventScroll: true })
            setOpen(false)
        }
        if (disabled) {
            closeMenu()
            return
        }
        const updatePlacement = () => {
            const rect = scroll.getBoundingClientRect()
            const top = Math.max(0, rect.top + scroll.clientTop)
            const bottom = Math.min(window.innerHeight, rect.top + scroll.clientTop + scroll.clientHeight)
            const placement = sessionMenuPlacement(anchor.getBoundingClientRect(), { top, bottom }, menu.scrollHeight + menu.offsetHeight - menu.clientHeight)
            if (scroll.clientWidth === 0 || !placement) {
                closeMenu()
                return
            }
            menu.dataset.placement = placement.placement
            menu.style.maxHeight = `${placement.maxHeight}px`
            menu.style.visibility = 'visible'
        }
        // 定位在首次绘制前完成，菜单不扩张侧栏的滚动范围。
        updatePlacement()
        menu.querySelector<HTMLButtonElement>('button')?.focus({ preventScroll: true })
        const closeOnOutsidePress = (event: PointerEvent) => {
            if (event.target instanceof Node && !anchor.contains(event.target)) closeMenu()
        }
        const closeOnScroll = () => closeMenu(menu.contains(document.activeElement))
        const onKeyDown = (event: KeyboardEvent) => {
            if (!anchor.contains(document.activeElement)) return
            if (event.key === 'Escape' || event.key === 'Tab') {
                if (event.key === 'Escape') {
                    event.preventDefault()
                    event.stopPropagation()
                }
                closeMenu(true)
                return
            }
            const items = Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
            const index = items.indexOf(document.activeElement as HTMLButtonElement)
            let next = -1
            switch (event.key) {
                case 'ArrowDown': next = (index + 1) % items.length; break
                case 'ArrowUp': next = (index - 1 + items.length) % items.length; break
                case 'Home': next = 0; break
                case 'End': next = items.length - 1; break
            }
            if (next >= 0) {
                event.preventDefault()
                items[next].focus()
            }
        }
        const observer = new ResizeObserver(updatePlacement)
        observer.observe(scroll)
        scroll.addEventListener('scroll', closeOnScroll)
        window.addEventListener('resize', updatePlacement)
        document.addEventListener('pointerdown', closeOnOutsidePress, true)
        document.addEventListener('keydown', onKeyDown)
        return () => {
            observer.disconnect()
            scroll.removeEventListener('scroll', closeOnScroll)
            window.removeEventListener('resize', updatePlacement)
            document.removeEventListener('pointerdown', closeOnOutsidePress, true)
            document.removeEventListener('keydown', onKeyDown)
        }
    }, [open, disabled])

    const closeMenu = () => {
        triggerRef.current?.focus({ preventScroll: true })
        setOpen(false)
    }
    return <>
        <div className="session-row-actions" onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false)
        }}>
            <button
                ref={triggerRef}
                className="icon-button session-menu-trigger"
                type="button"
                title="会话操作"
                aria-label={`打开会话 ${title || '未命名会话'} 操作`}
                aria-haspopup="menu"
                aria-expanded={open}
                aria-controls={open ? menuID : undefined}
                onClick={() => setOpen((current) => !current)}
                onKeyDown={(event) => {
                    if (event.key === 'ArrowDown') {
                        event.preventDefault()
                        event.stopPropagation()
                        setOpen(true)
                        if (open) menuRef.current?.querySelector<HTMLButtonElement>('button')?.focus()
                    }
                }}
                disabled={disabled}
            >
                <Ellipsis size={15} strokeWidth={1.8} aria-hidden="true" />
            </button>
            {open && <div className="session-actions-menu" role="menu" aria-label="会话操作" id={menuID} ref={menuRef}>
                <button type="button" role="menuitem" tabIndex={-1} className="session-action" onClick={() => { closeMenu(); setDialog('rename') }}>
                    <Pencil size={14} strokeWidth={1.8} aria-hidden="true" /><span>重命名会话</span>
                </button>
                <button type="button" role="menuitem" tabIndex={-1} className="session-action" onClick={() => { closeMenu(); void onRevealFile() }}>
                    <FolderOpen size={14} strokeWidth={1.8} aria-hidden="true" /><span>打开所在位置</span>
                </button>
                <button type="button" role="menuitem" tabIndex={-1} className="session-action-danger" onClick={() => { closeMenu(); setDialog('delete') }}>
                    <Trash2 size={14} strokeWidth={1.8} aria-hidden="true" /><span>删除会话</span>
                </button>
            </div>}
        </div>
        {dialog === 'delete' && <DeleteSessionDialog
            title={title || '未命名会话'}
            busy={busy}
            onClose={() => setDialog(null)}
            onConfirm={() => { setDialog(null); return onDelete() }}
        />}
        {dialog === 'rename' && <RenameSessionDialog
            initialTitle={title}
            busy={busy}
            onClose={() => setDialog(null)}
            onSubmit={async (title) => {
                if (await onRename(title)) setDialog(null)
            }}
        />}
    </>
}

type DeleteSessionDialogProps = {
    title: string
    busy: boolean
    onClose: () => void
    onConfirm: () => Promise<boolean>
}

function DeleteSessionDialog({ title, busy, onClose, onConfirm }: DeleteSessionDialogProps) {
    const titleID = useId()
    const messageID = useId()
    return (
        <Overlay labelledBy={titleID} onClose={() => { if (!busy) onClose() }}>
            <section className="project-dialog session-dialog" aria-describedby={messageID}>
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">会话操作</span>
                        <h2 id={titleID}>删除会话</h2>
                    </div>
                    <button className="icon-button dialog-close" type="button" title="关闭" aria-label="关闭删除会话对话框" onClick={onClose} disabled={busy}>
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                <p className="session-dialog-message" id={messageID}>确定删除会话“{title}”吗？此操作不可恢复。</p>
                <div className="modal-actions">
                    <button className="modal-secondary" type="button" onClick={onClose} disabled={busy}>取消</button>
                    <button className="modal-primary modal-danger" type="button" onClick={() => void onConfirm()} disabled={busy}>删除会话</button>
                </div>
            </section>
        </Overlay>
    )
}

type RenameSessionDialogProps = {
    initialTitle: string
    busy: boolean
    onClose: () => void
    onSubmit: (title: string) => Promise<void>
}

function RenameSessionDialog({ initialTitle, busy, onClose, onSubmit }: RenameSessionDialogProps) {
    const [title, setTitle] = useState(initialTitle)
    const titleID = useId()
    const inputID = useId()
    return (
        <Overlay labelledBy={titleID} onClose={() => { if (!busy) onClose() }}>
            <section className="project-dialog session-dialog">
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">会话操作</span>
                        <h2 id={titleID}>重命名会话</h2>
                    </div>
                    <button className="icon-button dialog-close" type="button" title="关闭" aria-label="关闭重命名会话对话框" onClick={onClose} disabled={busy}>
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                <form className="project-form" onSubmit={(event) => { event.preventDefault(); void onSubmit(title.trim()) }}>
                    <label htmlFor={inputID}>会话名称</label>
                    <input id={inputID} value={title} onChange={(event) => setTitle(event.target.value)} autoComplete="off" autoFocus required disabled={busy} />
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={onClose} disabled={busy}>取消</button>
                        <button className="modal-primary" type="submit" disabled={busy || !title.trim()}>保存</button>
                    </div>
                </form>
            </section>
        </Overlay>
    )
}
