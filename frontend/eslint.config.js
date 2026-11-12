import tseslint from 'typescript-eslint'
import reactHooks from 'eslint-plugin-react-hooks'

export default [{
    ignores: ['dist/**', 'node_modules/**', 'bindings/**'],
}, {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
        parser: tseslint.parser,
        parserOptions: {
            ecmaVersion: 'latest',
            sourceType: 'module',
            ecmaFeatures: { jsx: true },
            projectService: true,
            tsconfigRootDir: import.meta.dirname,
        },
    },
    plugins: { 'react-hooks': reactHooks, '@typescript-eslint': tseslint.plugin },
    rules: {
        'react-hooks/rules-of-hooks': 'error',
        'react-hooks/exhaustive-deps': 'error',
        '@typescript-eslint/consistent-type-imports': 'error',
        // void 只明确调用方不等待结果；终端操作仍须自行展示失败，不能据此认定已处理 rejection。
        '@typescript-eslint/no-floating-promises': ['error', { ignoreVoid: true }],
        '@typescript-eslint/no-misused-promises': 'error',
    },
}]
