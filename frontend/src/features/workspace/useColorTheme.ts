import { useCallback, useEffect, useEffectEvent, useState } from 'react'
import { applyColorTheme, readRememberedTheme, rememberColorTheme, resolveColorTheme, systemColorTheme, type ColorTheme } from './themePreference'

/** 管理工作区配色：显式选择后固定，未选择时跟随系统外观变化。 */
export function useColorTheme() {
    const [theme, setTheme] = useState<ColorTheme>(resolveColorTheme)

    useEffect(() => {
        applyColorTheme(theme)
    }, [theme])

    const followSystem = useEffectEvent(() => {
        // 用户显式选择过主题后不再被系统外观改写。
        if (readRememberedTheme()) return
        setTheme(systemColorTheme())
    })

    useEffect(() => {
        const query = window.matchMedia('(prefers-color-scheme: dark)')
        const onChange = () => followSystem()
        query.addEventListener('change', onChange)
        return () => query.removeEventListener('change', onChange)
    }, [])

    const toggleTheme = useCallback(() => {
        const next: ColorTheme = theme === 'dark' ? 'light' : 'dark'
        rememberColorTheme(next)
        setTheme(next)
    }, [theme])

    return { theme, toggleTheme }
}
