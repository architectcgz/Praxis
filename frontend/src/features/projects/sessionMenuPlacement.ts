type VerticalBounds = { top: number; bottom: number }

/** 按滚动区可见边界选择菜单方向和限高；锚点不可见或放不下一项操作时返回 null。 */
export function sessionMenuPlacement(anchor: VerticalBounds, bounds: VerticalBounds, menuHeight: number) {
    const gap = 4
    if (anchor.bottom <= bounds.top || anchor.top >= bounds.bottom) return null
    const above = Math.max(0, anchor.top - bounds.top - gap)
    const below = Math.max(0, bounds.bottom - anchor.bottom - gap)
    const placement = below < menuHeight && above > below ? 'top' : 'bottom'
    const maxHeight = Math.floor(placement === 'top' ? above : below)
    // 至少保留一项操作及菜单内边距、边框，过小的可见区域不展示残缺控件。
    return maxHeight >= 40 ? { placement, maxHeight } : null
}
