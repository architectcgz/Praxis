import { ReactNode, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

type OverlayProps = {
    labelledBy: string
    onClose: () => void
    children: ReactNode
    className?: string
}

const openOverlays = new Set<HTMLElement>()
let bodyOverflow = ''

export function Overlay({ labelledBy, onClose, children, className = '' }: OverlayProps) {
    const closeRef = useRef(onClose)
    const backdropRef = useRef<HTMLDivElement>(null)
    // 在子元素 autoFocus 执行前记录触发器，关闭后才能恢复到页面内的真实入口。
    const [previousActiveElement] = useState(() => document.activeElement instanceof HTMLElement ? document.activeElement : null)

    useEffect(() => {
        closeRef.current = onClose
    }, [onClose])

    useEffect(() => {
        const backdrop = backdropRef.current
        if (!backdrop) return
        // 抽屉可以打开确认弹窗；滚动锁直到最后一层关闭才释放。
        if (openOverlays.size === 0) {
            bodyOverflow = document.body.style.overflow
            document.body.style.overflow = 'hidden'
        }
        openOverlays.add(backdrop)
        const updateInteractivity = () => {
            for (const overlay of openOverlays) overlay.inert = overlay.parentElement?.lastElementChild !== overlay
        }
        updateInteractivity()
        const isTopOverlay = () => backdrop.parentElement?.lastElementChild === backdrop

        const focusableSelector = [
            'button:not([disabled])',
            '[href]',
            'input:not([disabled]):not([type="hidden"])',
            'select:not([disabled])',
            'textarea:not([disabled])',
            '[tabindex]:not([tabindex="-1"])',
        ].join(',')
        const focusableElements = () => Array.from(backdropRef.current?.querySelectorAll<HTMLElement>(focusableSelector) || [])
            .filter((element) => element.getClientRects().length > 0)
        const focusFirstElement = () => {
            const focusable = focusableElements()
            const first = focusable?.[0]
            if (first) {
                first.focus()
            } else {
                backdropRef.current?.focus()
            }
        }
        if (isTopOverlay() && !backdrop.contains(document.activeElement)) focusFirstElement()

        const handleKeyDown = (event: KeyboardEvent) => {
            if (!isTopOverlay() || event.defaultPrevented) return
            if (event.key === 'Escape') {
                event.preventDefault()
                event.stopPropagation()
                closeRef.current()
                return
            }
            if (event.key !== 'Tab') {
                return
            }
            const focusable = focusableElements()
            if (focusable.length === 0) {
                event.preventDefault()
                return
            }
            const first = focusable[0]
            const last = focusable[focusable.length - 1]
            if (!backdropRef.current?.contains(document.activeElement)) {
                event.preventDefault()
                first.focus()
            } else if (event.shiftKey && document.activeElement === first) {
                event.preventDefault()
                last.focus()
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault()
                first.focus()
            }
        }

        window.addEventListener('keydown', handleKeyDown)
        return () => {
            window.removeEventListener('keydown', handleKeyDown)
            openOverlays.delete(backdrop)
            updateInteractivity()
            if (openOverlays.size === 0) document.body.style.overflow = bodyOverflow
            const focusInAnotherOverlay = Array.from(document.querySelectorAll('#overlay-root > .modal-backdrop'))
                .some((overlay) => overlay !== backdrop && overlay.contains(document.activeElement))
            if (!focusInAnotherOverlay && previousActiveElement?.isConnected) previousActiveElement.focus({ preventScroll: true })
        }
    }, [previousActiveElement])

    const host = document.getElementById('overlay-root')
    if (!host) {
        return null
    }

    return createPortal(
        <div
            className={`modal-backdrop ${className}`.trim()}
            ref={backdropRef}
            tabIndex={-1}
            role="dialog"
            aria-modal="true"
            aria-labelledby={labelledBy}
            onMouseDown={(event) => {
                if (event.target === event.currentTarget) {
                    // 阻止卸载后的默认鼠标聚焦覆盖触发器焦点。
                    event.preventDefault()
                    closeRef.current()
                }
            }}
        >
            {children}
        </div>,
        host,
    )
}
