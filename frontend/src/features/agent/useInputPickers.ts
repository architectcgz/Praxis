import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

type InputPicker = 'provider' | 'model' | 'reasoning' | 'approval'

/** 输入区选择器共用互斥状态，并在所属 Region 内管理定位、键盘和监听生命周期。 */
export function useInputPickers({ busy, hasModels, reasoningAvailable }: {
    busy: boolean
    hasModels: boolean
    reasoningAvailable: boolean
}) {
    const [openPicker, setOpenPicker] = useState<InputPicker | null>(null)
    const providerPickerRef = useRef<HTMLDivElement>(null)
    const modelPickerRef = useRef<HTMLDivElement>(null)
    const reasoningPickerRef = useRef<HTMLDivElement>(null)
    const approvalPickerRef = useRef<HTMLDivElement>(null)
    const closePickers = useCallback(() => setOpenPicker(null), [])

    useLayoutEffect(() => {
        if (!openPicker) return
        const pickers = {
            provider: providerPickerRef,
            model: modelPickerRef,
            reasoning: reasoningPickerRef,
            approval: approvalPickerRef,
        }
        const picker = pickers[openPicker].current
        const popup = picker?.querySelector<HTMLElement>('.model-picker-menu, .thinking-intensity-picker')
        const trigger = picker?.querySelector<HTMLButtonElement>('button')
        const region = picker?.closest<HTMLElement>('.main-panel')
        if (!picker || !popup || !trigger) return

        const updatePlacement = () => {
            const bounds = region?.getBoundingClientRect()
            const anchor = trigger.getBoundingClientRect()
            const left = Math.max(8, bounds?.left ?? 0) + 4
            const right = Math.min(window.innerWidth - 8, bounds?.right ?? window.innerWidth) - 4
            const top = Math.max(8, bounds?.top ?? 0) + 4
            const bottom = Math.min(window.innerHeight - 8, bounds?.bottom ?? window.innerHeight) - 4
            const above = Math.max(0, anchor.top - top - 8)
            const below = Math.max(0, bottom - anchor.bottom - 8)
            if (right <= left || anchor.bottom <= top || anchor.top >= bottom || Math.max(above, below) < 40) {
                closePickers()
                return
            }
            popup.style.maxWidth = `${right - left}px`
            const height = Math.min(300, popup.scrollHeight + popup.offsetHeight - popup.clientHeight)
            const upward = above >= height || above >= below
            popup.style.maxHeight = `${Math.min(300, upward ? above : below)}px`
            popup.style.top = upward ? 'auto' : 'calc(100% + 8px)'
            popup.style.bottom = upward ? 'calc(100% + 8px)' : 'auto'
            const x = Math.max(left, Math.min(anchor.left + (anchor.width - popup.offsetWidth) / 2, right - popup.offsetWidth))
            popup.style.left = `${x - picker.getBoundingClientRect().left}px`
            popup.style.visibility = 'visible'
        }
        // 在首次绘制前约束到所属 Region，避免祖先裁剪或跨栏遮挡。
        updatePlacement()
        const selected = popup.querySelector<HTMLElement>('[aria-current="true"]') || popup.querySelector<HTMLElement>('input, button')
        selected?.focus()

        const closeOnOutsidePress = (event: PointerEvent) => {
            if (event.target instanceof Node && !picker.contains(event.target)) closePickers()
        }
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' || event.key === 'Tab') {
                if (event.key === 'Escape') {
                    event.preventDefault()
                    event.stopPropagation()
                }
                trigger.focus({ preventScroll: true })
                closePickers()
                return
            }
            if (openPicker === 'reasoning' || !popup.contains(document.activeElement)) return
            const items = Array.from(popup.querySelectorAll<HTMLButtonElement>('button'))
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
                items[next]?.focus()
            }
        }
        const closeOnFocusExit = (event: FocusEvent) => {
            if (event.target instanceof Node && !picker.contains(event.target)) closePickers()
        }
        const closeOnScroll = (event: Event) => {
            if (event.target instanceof Node && popup.contains(event.target)) return
            if (popup.contains(document.activeElement)) trigger.focus({ preventScroll: true })
            closePickers()
        }
        const observer = new ResizeObserver(updatePlacement)
        observer.observe(picker)
        if (region) observer.observe(region)
        window.addEventListener('resize', updatePlacement)
        document.addEventListener('pointerdown', closeOnOutsidePress, true)
        document.addEventListener('keydown', onKeyDown)
        document.addEventListener('focusin', closeOnFocusExit)
        document.addEventListener('scroll', closeOnScroll, true)
        return () => {
            observer.disconnect()
            window.removeEventListener('resize', updatePlacement)
            document.removeEventListener('pointerdown', closeOnOutsidePress, true)
            document.removeEventListener('keydown', onKeyDown)
            document.removeEventListener('focusin', closeOnFocusExit)
            document.removeEventListener('scroll', closeOnScroll, true)
        }
    }, [openPicker, closePickers])

    useEffect(() => {
        setOpenPicker((current) => {
            if (busy) return null
            if (!hasModels && (current === 'provider' || current === 'model')) return null
            if (!reasoningAvailable && current === 'reasoning') return null
            return current
        })
    }, [busy, hasModels, reasoningAvailable])

    return {
        openPicker,
        togglePicker: (picker: InputPicker) => setOpenPicker((current) => current === picker ? null : picker),
        closePickers,
        providerPickerRef,
        modelPickerRef,
        reasoningPickerRef,
        approvalPickerRef,
    }
}
