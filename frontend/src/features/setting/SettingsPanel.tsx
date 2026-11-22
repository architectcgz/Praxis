import { Cpu, FileJson, RefreshCw } from 'lucide-react'
import type { ModelOption } from '../../api'

type SettingsPanelProps = {
    models: ModelOption[]
    hasActiveSession: boolean
}

const configFiles = [
    { path: '~/.praxis/config/models.json', description: 'Provider、分组、模型目录和全局默认模型（defaultModel）' },
    { path: '~/.praxis/config/auth.json', description: 'Provider API 凭据（仅后端本地读取，不暴露至界面）' },
    { path: '~/.praxis/config/settings.json', description: '全局默认工具审批模式（ask / auto / yolo）' },
    { path: '~/.praxis/config/tools.json', description: '工具启停策略（如按工具名禁用 bash 等内置工具）' },
    { path: '~/.praxis/config/agents/', description: '主代理与自定义子代理角色定义目录' },
] as const

export function SettingsPanel({ models, hasActiveSession }: SettingsPanelProps) {
    const providerCount = new Set(models.map((item) => item.providerId)).size
    return (
        <section className="region settings-region" aria-labelledby="settings-title">
            <div className="scroll-region">
                <div className="page settings-content">
                    <div className="settings-grid">
                        <section className="settings-card" aria-labelledby="settings-models-title">
                            <div className="settings-card-header">
                                <div className="settings-card-icon is-primary" aria-hidden="true">
                                    <Cpu size={16} strokeWidth={1.8} />
                                </div>
                                <div className="settings-card-header-text">
                                    <h2 id="settings-models-title">已加载模型目录</h2>
                                    <p>会话优先恢复手动选择；未选择时使用全局默认模型。</p>
                                </div>
                            </div>
                            <div className="settings-card-body">
                                {hasActiveSession ? (
                                    <>
                                        <div className="settings-card-stats">
                                            <div className="settings-stat">
                                                <span className="settings-stat-value">{providerCount}</span>
                                                <span className="settings-stat-label">Provider</span>
                                            </div>
                                            <div className="settings-stat">
                                                <span className="settings-stat-value">{models.length}</span>
                                                <span className="settings-stat-label">可用模型</span>
                                            </div>
                                        </div>
                                        {models.length > 0 ? (
                                            <ul className="settings-model-list">
                                                {models.map((model) => (
                                                    <li className="settings-model-row" key={`${model.providerId}:${model.modelId}`}>
                                                        <div className="settings-model-main">
                                                            <strong>{model.label}</strong>
                                                            <code>{model.providerName} / {model.modelId}</code>
                                                        </div>
                                                        <span className="settings-model-reasoning">
                                                            {model.reasoningLevels.length > 0
                                                                ? `推理: ${model.reasoningLevels.join(' · ')}`
                                                                : '无推理档位'}
                                                        </span>
                                                    </li>
                                                ))}
                                            </ul>
                                        ) : (
                                            <p className="settings-empty-note">当前会话未加载任何可用模型，请检查 models.json 与 auth.json 后在会话内执行 <code>/reload</code>。</p>
                                        )}
                                    </>
                                ) : (
                                    <p className="settings-empty-note">模型目录按会话快照加载。打开或新建一个会话后，可在此查看该会话已加载的 Provider 与模型列表，并在输入区随时切换。</p>
                                )}
                            </div>
                        </section>

                        <section className="settings-card" aria-labelledby="settings-reload-title">
                            <div className="settings-card-header">
                                <div className="settings-card-icon is-primary" aria-hidden="true">
                                    <RefreshCw size={16} strokeWidth={1.8} />
                                </div>
                                <div className="settings-card-header-text">
                                    <h2 id="settings-reload-title">配置快照与热重载</h2>
                                    <p>编辑配置文件不会影响正在执行的任务或其他已打开会话。</p>
                                </div>
                            </div>
                            <div className="settings-card-body">
                                <ul className="settings-rules-list">
                                    <li>已加载会话使用各自打开时的只读配置快照，正在执行的 Run 参数固定不变。</li>
                                    <li>保存配置文件后，可在目标会话空闲时输入 <code>/reload</code> 单独刷新该会话配置。</li>
                                    <li>存在活动执行或待审批调用时，<code>/reload</code> 会被拒绝；重载成功后会清理未开始的待办队列。</li>
                                    <li>会话内修改的审批覆盖（<code>ask / auto / yolo</code>）同样在执行 <code>/reload</code> 后生效。</li>
                                </ul>
                            </div>
                        </section>

                        <section className="settings-info-card" aria-labelledby="settings-files-title">
                            <div className="settings-info-icon" aria-hidden="true">
                                <FileJson size={18} strokeWidth={1.8} />
                            </div>
                            <div className="settings-info-content">
                                <strong id="settings-files-title">本地配置文件位置</strong>
                                <p>所有配置均保存在用户主目录下的 <code>.praxis/config/</code>，可直接使用编辑器维护：</p>
                                <dl className="settings-file-list">
                                    {configFiles.map((file) => (
                                        <div className="settings-file-row" key={file.path}>
                                            <dt><code>{file.path}</code></dt>
                                            <dd>{file.description}</dd>
                                        </div>
                                    ))}
                                </dl>
                            </div>
                        </section>
                    </div>
                </div>
            </div>
        </section>
    )
}
