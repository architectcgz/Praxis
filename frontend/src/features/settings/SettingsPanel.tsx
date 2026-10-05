import { AlertTriangle, CheckCircle2, ChevronRight, Cpu, FileJson, Settings2 } from 'lucide-react'
import { ModelOption } from '../../api'

type SettingsPanelProps = {
    models: ModelOption[]
    onOpenModels: () => void
}

export function SettingsPanel({ models, onOpenModels }: SettingsPanelProps) {
    const configured = models.some((model) => model.assignedAgentDefinitions.includes('primary'))
    const providers = new Set(models.map((model) => model.providerName)).size
    return (
        <section className="region settings-region" aria-labelledby="settings-title">
            <div className="scroll-region">
                <div className="page page-header settings-hero">
                    <div className="settings-hero-content">
                        <div className="settings-hero-icon">
                            <Settings2 size={28} strokeWidth={2} />
                        </div>
                        <div className="settings-hero-text">
                            <span className="settings-hero-kicker">配置</span>
                            <h1 id="settings-title">工作区设置</h1>
                            <p>配置您的推理 Provider、Model 和工作区偏好</p>
                        </div>
                    </div>
                    {configured && (
                        <div className="settings-status-badge is-success">
                            <CheckCircle2 size={14} />
                            <span>系统就绪</span>
                        </div>
                    )}
                </div>

                <div className="page settings-content">
                    {!configured && (
                        <div className="settings-alert" role="alert">
                            <div className="settings-alert-content">
                                <div className="settings-alert-icon">
                                    <AlertTriangle size={20} strokeWidth={2} />
                                </div>
                                <div className="settings-alert-body">
                                    <strong>需要操作</strong>
                                    <p>配置主 Model 以启用 Agent 会话。<code>agents/primary/agent.json</code> 必须绑定 Model。</p>
                                </div>
                            </div>
                        </div>
                    )}

                    <div className="settings-grid">
                        <div className="settings-card">
                            <div className="settings-card-header">
                                <div className="settings-card-icon is-primary">
                                    <Cpu size={22} strokeWidth={2} />
                                </div>
                                <div className="settings-card-header-text">
                                    <h2>Model 与 Provider</h2>
                                    <p>管理推理 Model 和 API Provider</p>
                                </div>
                            </div>
                            <div className="settings-card-body">
                                <div className="settings-card-stats">
                                    <div className="settings-stat">
                                        <span className="settings-stat-value">{models.length}</span>
                                        <span className="settings-stat-label">Model</span>
                                    </div>
                                    <div className="settings-stat">
                                        <span className="settings-stat-value">{providers}</span>
                                        <span className="settings-stat-label">Provider</span>
                                    </div>
                                    <div className="settings-stat">
                                        <div className={`settings-stat-badge ${configured ? 'is-success' : 'is-warning'}`}>
                                            {configured ? '就绪' : '需要设置'}
                                        </div>
                                    </div>
                                </div>
                            </div>
                            <div className="settings-card-footer">
                                <button className="settings-card-action" type="button" onClick={onOpenModels}>
                                    <span>配置 Model</span>
                                    <ChevronRight size={16} />
                                </button>
                            </div>
                        </div>

                        <div className="settings-info-card">
                            <div className="settings-info-icon">
                                <FileJson size={18} />
                            </div>
                            <div className="settings-info-content">
                                <strong>配置存储</strong>
                                <p>更改保存到 <code>~/.praxis/config/models.json</code> 并立即生效。</p>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </section>
    )
}
