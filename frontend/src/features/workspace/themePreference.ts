const rememberedThemeKey = 'praxis.color-theme'

export type ColorTheme = 'light' | 'dark'

/** 读取用户显式选择过的主题；未选择过时返回空串，由调用方决定回退策略。 */
export function readRememberedTheme(): ColorTheme | '' {
    try {
        const value = window.localStorage.getItem(rememberedThemeKey)
        return value === 'light' || value === 'dark' ? value : ''
    } catch {
        return ''
    }
}

export function rememberColorTheme(theme: ColorTheme) {
    try {
        window.localStorage.setItem(rememberedThemeKey, theme)
    } catch {
        // localStorage 在浏览器预览和 Wails webview 中可能不可用。
    }
}

/** 未显式选择过主题时跟随系统外观。 */
export function systemColorTheme(): ColorTheme {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveColorTheme(): ColorTheme {
    return readRememberedTheme() || systemColorTheme()
}

/** 主题只体现在 <html data-theme> 上，CSS 按该属性切换同名令牌取值。 */
export function applyColorTheme(theme: ColorTheme) {
    document.documentElement.dataset.theme = theme
}
