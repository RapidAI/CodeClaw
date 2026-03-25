import { useState, useEffect, useCallback } from 'react'
import { Cloud, ArrowRight, ExternalLink, Info, CheckCircle2, XCircle, RefreshCw } from 'lucide-react'
import clsx from 'clsx'
import { useI18n } from '../i18n'

export default function VercelSync({ onMessage, authFetch }) {
    const { t } = useI18n()
    const [vercelToken, setVercelToken] = useState('')
    const [projectId, setProjectId] = useState('')
    const [teamId, setTeamId] = useState('')
    const [loading, setLoading] = useState(false)
    const [result, setResult] = useState(null)
    const [preconfig, setPreconfig] = useState(null)
    const [syncStatus, setSyncStatus] = useState(null)

    const apiFetch = authFetch || fetch

    const fetchSyncStatus = useCallback(async () => {
        try {
            const res = await apiFetch('/admin/vercel/status')
            if (res.ok) {
                const data = await res.json()
                setSyncStatus(data)
            }
        } catch (e) {
            console.error('Failed to fetch sync status:', e)
        }
    }, [apiFetch])

    useEffect(() => {
        const loadPreconfig = async () => {
            try {
                const res = await apiFetch('/admin/vercel/config')
                if (res.ok) {
                    const data = await res.json()
                    setPreconfig(data)
                    if (data.project_id) setProjectId(data.project_id)
                    if (data.team_id) setTeamId(data.team_id)
                }
            } catch (e) {
                console.error('Failed to load preconfig:', e)
            }
        }
        loadPreconfig()
        fetchSyncStatus()
        // Poll every 15s to detect config changes
        const interval = setInterval(fetchSyncStatus, 15000)
        return () => clearInterval(interval)
    }, [fetchSyncStatus])

    const handleSync = async () => {
        const tokenToUse = preconfig?.has_token && !vercelToken ? '__USE_PRECONFIG__' : vercelToken

        if (!tokenToUse && !preconfig?.has_token) {
            onMessage('error', t('vercel.tokenRequired'))
            return
        }
        if (!projectId) {
            onMessage('error', t('vercel.projectRequired'))
            return
        }

        setLoading(true)
        setResult(null)
        try {
            const res = await apiFetch('/admin/vercel/sync', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    vercel_token: tokenToUse,
                    project_id: projectId,
                    team_id: teamId || undefined,
                }),
            })
            const data = await res.json()
            if (res.ok) {
                setResult({ ...data, success: true })
                onMessage('success', data.message)
                fetchSyncStatus()
            } else {
                setResult({ ...data, success: false })
                onMessage('error', data.detail || t('vercel.syncFailed'))
            }
        } catch (e) {
            onMessage('error', t('vercel.networkError'))
        } finally {
            setLoading(false)
        }
    }

    return (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 max-w-5xl mx-auto h-[calc(100vh-140px)]">
            {/* Configuration Form */}
            <div className="bg-card border border-border rounded-xl shadow-sm p-6 space-y-6">
                <div className="border-b border-border pb-6">
                    <div className="flex items-center justify-between">
                        <h2 className="text-xl font-semibold flex items-center gap-2">
                            <Cloud className="w-6 h-6 text-primary" />
                            {t('vercel.title')}
                        </h2>
                        {syncStatus && (
                            <div className={clsx(
                                "flex items-center gap-1.5 text-xs font-semibold px-2.5 py-1 rounded-full border transition-colors",
                                syncStatus.synced
                                    ? "text-emerald-500 bg-emerald-500/10 border-emerald-500/20"
                                    : syncStatus.has_synced_before
                                        ? "text-amber-500 bg-amber-500/10 border-amber-500/20"
                                        : "text-muted-foreground bg-muted/50 border-border"
                            )}>
                                <span className={clsx(
                                    "w-1.5 h-1.5 rounded-full",
                                    syncStatus.synced ? "bg-emerald-500" : syncStatus.has_synced_before ? "bg-amber-500 animate-pulse" : "bg-muted-foreground"
                                )} />
                                {syncStatus.synced
                                    ? t('vercel.statusSynced')
                                    : syncStatus.has_synced_before
                                        ? t('vercel.statusNotSynced')
                                        : t('vercel.statusNeverSynced')}
                            </div>
                        )}
                    </div>
                    <p className="text-muted-foreground text-sm mt-1">
                        {t('vercel.description')}
                    </p>
                    {syncStatus?.last_sync_time && (
                        <p className="text-xs text-muted-foreground/60 mt-1.5 flex items-center gap-1">
                            <RefreshCw className="w-3 h-3" />
                            {t('vercel.lastSyncTime', { time: new Date(syncStatus.last_sync_time * 1000).toLocaleString() })}
                        </p>
                    )}
                </div>

                <div className="space-y-4">
                    <div className="space-y-2">
                        <label className="text-sm font-medium flex items-center justify-between">
                            {t('vercel.tokenLabel')}
                            <a href="https://vercel.com/account/tokens" target="_blank" rel="noopener noreferrer" className="text-xs text-primary hover:underline flex items-center gap-1">
                                {t('vercel.getToken')} <ExternalLink className="w-3 h-3" />
                            </a>
                        </label>
                        <div className="relative">
                            <input
                                type="password"
                                className="w-full h-10 px-3 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-ring focus:border-ring transition-all pr-10"
                                placeholder={preconfig?.has_token ? t('vercel.tokenPlaceholderPreconfig') : t('vercel.tokenPlaceholder')}
                                value={vercelToken}
                                onChange={e => setVercelToken(e.target.value)}
                            />
                            {preconfig?.has_token && !vercelToken && (
                                <div className="absolute right-3 top-2.5 text-emerald-500">
                                    <CheckCircle2 className="w-5 h-5" />
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="space-y-2">
                        <label className="text-sm font-medium">{t('vercel.projectIdLabel')}</label>
                        <input
                            type="text"
                            className="w-full h-10 px-3 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-ring focus:border-ring transition-all"
                            placeholder="prj_xxxxxxxxxxxx or Project Name"
                            value={projectId}
                            onChange={e => setProjectId(e.target.value)}
                        />
                        <p className="text-xs text-muted-foreground">{t('vercel.projectIdHint')}</p>
                    </div>

                    <div className="space-y-2">
                        <label className="text-sm font-medium flex items-center gap-2">
                            {t('vercel.teamIdLabel')} <span className="text-xs text-muted-foreground font-normal">({t('vercel.optional')})</span>
                        </label>
                        <input
                            type="text"
                            className="w-full h-10 px-3 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-ring focus:border-ring transition-all"
                            placeholder="team_xxxxxxxxxxxx"
                            value={teamId}
                            onChange={e => setTeamId(e.target.value)}
                        />
                    </div>
                </div>

                <div className="pt-4">
                    <button
                        onClick={handleSync}
                        disabled={loading}
                        className="w-full flex items-center justify-center gap-2 py-3 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-all font-medium text-sm shadow-sm hover:shadow-md disabled:opacity-50 disabled:shadow-none"
                    >
                        {loading ? (
                            <span className="flex items-center gap-2">
                                <span className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin" />
                                {t('vercel.syncing')}
                            </span>
                        ) : (
                            <span className="flex items-center gap-2">
                                {t('vercel.syncRedeploy')} <ArrowRight className="w-4 h-4" />
                            </span>
                        )}
                    </button>
                    <p className="text-xs text-center text-muted-foreground mt-4">
                        {t('vercel.redeployHint')}
                    </p>
                </div>
            </div>

            {/* Status & Guide */}
            <div className="space-y-6">
                {result && (
                    <div className={`p-6 rounded-xl border ${result.success ? 'bg-emerald-500/10 border-emerald-500/20' : 'bg-destructive/10 border-destructive/20'} animate-in fade-in slide-in-from-right-4`}>
                        <div className="flex items-start gap-4">
                            {result.success ? (
                                <div className="p-2 bg-emerald-500 text-white rounded-full shadow-lg shadow-emerald-500/30">
                                    <CheckCircle2 className="w-6 h-6" />
                                </div>
                            ) : (
                                <div className="p-2 bg-destructive text-white rounded-full shadow-lg shadow-destructive/30">
                                    <XCircle className="w-6 h-6" />
                                </div>
                            )}
                            <div className="space-y-1">
                                <h3 className={`font-semibold text-lg ${result.success ? 'text-emerald-500' : 'text-destructive'}`}>
                                    {result.success ? t('vercel.syncSucceeded') : t('vercel.syncFailedLabel')}
                                </h3>
                                <p className="text-sm opacity-90">{result.message}</p>

                                {result.deployment_url && (
                                    <div className="pt-3 mt-3 border-t border-emerald-500/20">
                                        <a href={`https://${result.deployment_url}`} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm font-medium hover:underline">
                                            {t('vercel.openDeployment')} <ExternalLink className="w-3 h-3" />
                                        </a>
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>
                )}

                <div className="bg-secondary/20 border border-border rounded-xl p-6">
                    <h3 className="font-semibold flex items-center gap-2 mb-4">
                        <Info className="w-5 h-5 text-primary" />
                        {t('vercel.howItWorks')}
                    </h3>
                    <ul className="space-y-4">
                        <li className="flex gap-3">
                            <span className="shrink-0 w-6 h-6 rounded-full bg-background border border-border flex items-center justify-center text-xs font-bold text-muted-foreground">1</span>
                            <p className="text-sm text-muted-foreground">{t('vercel.steps.one')}</p>
                        </li>
                        <li className="flex gap-3">
                            <span className="shrink-0 w-6 h-6 rounded-full bg-background border border-border flex items-center justify-center text-xs font-bold text-muted-foreground">2</span>
                            <p className="text-sm text-muted-foreground">{t('vercel.steps.two')}</p>
                        </li>
                        <li className="flex gap-3">
                            <span className="shrink-0 w-6 h-6 rounded-full bg-background border border-border flex items-center justify-center text-xs font-bold text-muted-foreground">3</span>
                            <p className="text-sm text-muted-foreground">
                                {t('vercel.steps.three')} <code className="bg-background px-1 py-0.5 rounded border border-border text-xs">DS2API_CONFIG_JSON</code>
                            </p>
                        </li>
                        <li className="flex gap-3">
                            <span className="shrink-0 w-6 h-6 rounded-full bg-background border border-border flex items-center justify-center text-xs font-bold text-muted-foreground">4</span>
                            <p className="text-sm text-muted-foreground">{t('vercel.steps.four')}</p>
                        </li>
                    </ul>
                </div>
            </div>
        </div>
    )
}
