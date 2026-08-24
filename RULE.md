# Frontend Rules

- 禁止在 `frontend/` 使用 CSS `!important`。
- 样式冲突必须通过组件范围、选择器层级、样式入口顺序或 CSS 自定义属性解决，不得通过 `!important` 提升优先级。
- `prefers-reduced-motion` 必须通过可继承的 motion token 或受控组件选择器实现，不能使用全局 `!important` 覆盖。
