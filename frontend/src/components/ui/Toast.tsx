import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { CircleCheck } from 'lucide-react'

type ToastProps = {
    message: string
    onDismiss: () => void
}

/** 展示非阻塞的操作完成提示，3 秒后通知调用方清空消息；消息变化或卸载时清理计时器。 */
export function Toast({ message, onDismiss }: ToastProps) {
    useEffect(() => {
        if (!message) return
        const timer = window.setTimeout(onDismiss, 3000)
        return () => window.clearTimeout(timer)
    }, [message, onDismiss])

    if (!message) return null

    return createPortal(
        <div className="toast-notice" role="status" aria-atomic="true">
            <CircleCheck size={18} strokeWidth={1.8} aria-hidden="true" />
            <span>{message}</span>
        </div>,
        document.body,
    )
}
