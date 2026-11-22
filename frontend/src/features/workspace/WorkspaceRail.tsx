import { Folder, Globe, Layers, Moon, Settings, Sun } from 'lucide-react'
import { useColorTheme } from './useColorTheme'

export type WorkspacePage = 'project' | 'session' | 'settings' | 'world'

type WorkspaceRailProps = {
    activePage: WorkspacePage
    navigationOpen: boolean
    onToggleNavigation: () => void
    onOpenWorkbench: () => void
    onOpenWorld: () => void
    onOpenSettings: () => void
}

/** F2 左侧 52px 常驻功能导轨：统一锚定同层项目目录开关、工作台、世界页、设置与主题切换。 */
export function WorkspaceRail({
    activePage,
    navigationOpen,
    onToggleNavigation,
    onOpenWorkbench,
    onOpenWorld,
    onOpenSettings,
}: WorkspaceRailProps) {
    const { theme, toggleTheme } = useColorTheme()
    const nextThemeLabel = theme === 'dark' ? '切换到白昼薄雾主题' : '切换到深空暮色主题'

    return (
        <aside className="app-rail" aria-label="工作区主导航">
            <div className="rail-logo" aria-hidden="true">P</div>

            <button
                className={`rail-btn ${activePage === 'project' ? 'is-active' : navigationOpen ? 'is-open' : ''}`}
                type="button"
                title={navigationOpen ? '收起项目目录 (Ctrl+K)' : '展开项目目录 (Ctrl+K)'}
                aria-label={navigationOpen ? '收起项目目录' : '展开项目目录'}
                aria-expanded={navigationOpen}
                aria-controls={navigationOpen ? 'workspace-navigation' : undefined}
                onClick={onToggleNavigation}
            >
                <Folder size={17} strokeWidth={1.8} aria-hidden="true" />
            </button>

            <button
                className={`rail-btn ${activePage === 'session' ? 'is-active' : ''}`}
                type="button"
                title="工作台"
                aria-label="工作台"
                aria-current={activePage === 'session' ? 'page' : undefined}
                onClick={onOpenWorkbench}
            >
                <Layers size={17} strokeWidth={1.8} aria-hidden="true" />
            </button>

            <button
                className={`rail-btn ${activePage === 'world' ? 'is-active' : ''}`}
                type="button"
                title="共生推演世界"
                aria-label="共生推演世界"
                aria-current={activePage === 'world' ? 'page' : undefined}
                onClick={onOpenWorld}
            >
                <Globe size={17} strokeWidth={1.8} aria-hidden="true" />
            </button>

            <div className="rail-spacer" />
            <div className="rail-sep" aria-hidden="true" />

            <button
                className={`rail-btn ${activePage === 'settings' ? 'is-active' : ''}`}
                type="button"
                title="工作区设置"
                aria-label="工作区设置"
                aria-current={activePage === 'settings' ? 'page' : undefined}
                onClick={onOpenSettings}
            >
                <Settings size={17} strokeWidth={1.8} aria-hidden="true" />
            </button>

            <button
                className="rail-btn"
                type="button"
                title={nextThemeLabel}
                aria-label={nextThemeLabel}
                aria-pressed={theme === 'dark'}
                onClick={toggleTheme}
            >
                {theme === 'dark'
                    ? <Sun size={17} strokeWidth={1.8} aria-hidden="true" />
                    : <Moon size={17} strokeWidth={1.8} aria-hidden="true" />}
            </button>
        </aside>
    )
}
