import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'

type SlashCommand = { name: string; description: string }

/** 输入框内的命令候选只负责选择；执行与错误处理由工作区负责。 */
export function useSlashCommandMenu(
    input: string,
    setInput: (value: string) => void,
    commands: readonly SlashCommand[],
    onCommand: (name: string) => Promise<boolean>,
    disabled: boolean,
) {
    const menuRef = useRef<HTMLDivElement>(null)
    const [dismissed, setDismissed] = useState('')
    const [selected, setSelected] = useState(0)
    const options = /^\/[a-z]*$/i.test(input) ? commands.filter((command) => command.name.startsWith(input.toLowerCase())) : []
    const open = !disabled && input !== dismissed && options.length > 0
    const active = Math.min(selected, options.length - 1)

    useLayoutEffect(() => {
        const menu = menuRef.current
        const band = menu?.closest<HTMLElement>('.command-band')
        const region = menu?.closest<HTMLElement>('.main-panel')
        if (!open || !menu || !band) return
        const place = () => {
            const top = Math.max(8, region?.getBoundingClientRect().top ?? 0)
            menu.style.maxHeight = `${Math.max(0, Math.min(220, band.getBoundingClientRect().top - top - 6))}px`
        }
        place()
        const observer = new ResizeObserver(place)
        observer.observe(band)
        if (region) observer.observe(region)
        window.addEventListener('resize', place)
        return () => {
            observer.disconnect()
            window.removeEventListener('resize', place)
        }
    }, [open])

    useEffect(() => {
        if (!open) return
        const closeOutside = (event: PointerEvent) => {
            if (event.target instanceof Node && !menuRef.current?.contains(event.target) &&
                !(event.target instanceof HTMLTextAreaElement && event.target.id === 'agent-input')) {
                setDismissed(input)
            }
        }
        const closeOnFocusExit = (event: FocusEvent) => {
            if (event.target instanceof Node && !menuRef.current?.contains(event.target) &&
                !(event.target instanceof HTMLTextAreaElement && event.target.id === 'agent-input')) setDismissed(input)
        }
        document.addEventListener('pointerdown', closeOutside)
        document.addEventListener('focusin', closeOnFocusExit)
        return () => {
            document.removeEventListener('pointerdown', closeOutside)
            document.removeEventListener('focusin', closeOnFocusExit)
        }
    }, [input, open])

    const pick = (name: string) => {
        setDismissed(input)
        void onCommand(name)
    }

    const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
        if (!open || event.nativeEvent.isComposing) return false
        if (event.key === 'Escape') {
            event.preventDefault()
            setDismissed(input)
            return true
        }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault()
            setSelected((current) => (current + (event.key === 'ArrowDown' ? 1 : options.length - 1)) % options.length)
            return true
        }
        if (event.key === 'Tab' && !event.shiftKey) {
            event.preventDefault()
            setInput(options[active].name)
            setSelected(0)
            return true
        }
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault()
            pick(options[active].name)
            return true
        }
        return false
    }

    const changeInput = (value: string) => {
        setSelected(0)
        setDismissed('')
        setInput(value)
    }

    const menu = (
        <>
            {open && (
                <div className="slash-command-menu" id="slash-command-menu" role="listbox" aria-label="工作区命令" ref={menuRef} onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                        event.preventDefault()
                        setDismissed(input)
                        document.getElementById('agent-input')?.focus()
                    }
                }}>
                    {options.map((command, index) => (
                        <button
                            className={`slash-command-option ${index === active ? 'is-selected' : ''}`}
                            type="button"
                            role="option"
                            id={`slash-command-option-${index}`}
                            aria-selected={index === active}
                            key={command.name}
                            onMouseDown={(event) => event.preventDefault()}
                            onClick={() => pick(command.name)}
                        >
                            <span className="slash-command-name">{command.name}</span>
                            <span className="slash-command-description">{command.description}</span>
                        </button>
                    ))}
                </div>
            )}
        </>
    )

    return {
        menu,
        changeInput,
        handleKeyDown,
        menuOpen: open,
        selectedOptionID: open ? `slash-command-option-${active}` : undefined,
    }
}
