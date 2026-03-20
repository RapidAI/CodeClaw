import { useState, useEffect, useMemo, useCallback, useRef } from 'react';

interface GossipPost {
    id: string;
    nickname: string;
    content: string;
    category: string;
    score: number;
    votes: number;
    locked: boolean;
    created_at: string;
}

interface GossipPanelProps {
    hubUrl: string;
    lang: string;
}

type SortMode = 'newest' | 'hottest' | 'score';

const PAGE_SIZE = 10;
const POLL_INTERVAL = 30_000; // 30s

const t = (lang: string, zh: string, en: string) => lang?.startsWith('zh') ? zh : en;

const categoryLabel = (lang: string, cat: string) => {
    const map: Record<string, Record<string, string>> = {
        owner:   { zh: '吐槽老板', en: 'Boss Talk' },
        project: { zh: '项目八卦', en: 'Project Gossip' },
        news:    { zh: '业界新闻', en: 'Industry News' },
    };
    return map[cat]?.[lang?.startsWith('zh') ? 'zh' : 'en'] ?? cat;
};

export function GossipPanel({ hubUrl, lang }: GossipPanelProps) {
    const [posts, setPosts] = useState<GossipPost[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [search, setSearch] = useState('');
    const [sort, setSort] = useState<SortMode>('newest');
    const [page, setPage] = useState(1);
    const etagRef = useRef('');

    const fetchSnapshot = useCallback(async (isPolling = false) => {
        if (!hubUrl) return;
        if (!isPolling) setLoading(true);
        try {
            const headers: Record<string, string> = {};
            if (etagRef.current) headers['If-None-Match'] = etagRef.current;
            const resp = await fetch(`${hubUrl.replace(/\/+$/, '')}/api/gossip/snapshot`, { headers });
            if (resp.status === 304) return; // unchanged
            if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
            const etag = resp.headers.get('ETag');
            if (etag) etagRef.current = etag;
            const data = await resp.json();
            setPosts(data.posts || []);
            setError('');
        } catch (err: any) {
            if (!isPolling) setError(err.message || 'Failed to load');
        } finally {
            if (!isPolling) setLoading(false);
        }
    }, [hubUrl]);

    // Initial fetch
    useEffect(() => {
        fetchSnapshot();
    }, [fetchSnapshot]);

    // Polling
    useEffect(() => {
        if (!hubUrl) return;
        const timer = setInterval(() => fetchSnapshot(true), POLL_INTERVAL);
        return () => clearInterval(timer);
    }, [hubUrl, fetchSnapshot]);

    // Filtered + sorted + paginated
    const filtered = useMemo(() => {
        let list = posts;
        if (search.trim()) {
            const q = search.trim().toLowerCase();
            list = list.filter(p =>
                p.nickname.toLowerCase().includes(q) ||
                p.content.toLowerCase().includes(q) ||
                categoryLabel(lang, p.category).toLowerCase().includes(q)
            );
        }
        if (sort === 'newest') list = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
        else if (sort === 'hottest') list = [...list].sort((a, b) => b.votes - a.votes || b.score - a.score);
        else if (sort === 'score') list = [...list].sort((a, b) => b.score - a.score || b.votes - a.votes);
        return list;
    }, [posts, search, sort, lang]);

    const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    const currentPage = Math.min(page, totalPages);
    const pageItems = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);

    // Reset page when search/sort changes
    useEffect(() => { setPage(1); }, [search, sort]);

    if (!hubUrl) {
        return <div style={{ padding: '40px 20px', textAlign: 'center', color: '#9ca3af', fontSize: '0.85rem' }}>
            {t(lang, '请先配置 Hub Center 地址', 'Please configure Hub Center URL first')}
        </div>;
    }

    return (
        <div style={{ padding: '0 15px', width: '100%', boxSizing: 'border-box' }}>
            {/* Toolbar */}
            <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '12px', flexWrap: 'wrap' }}>
                <input
                    type="text"
                    value={search}
                    onChange={e => setSearch(e.target.value)}
                    placeholder={t(lang, '搜索昵称/内容/分类...', 'Search nickname/content/category...')}
                    style={{ flex: 1, minWidth: '140px', padding: '5px 10px', borderRadius: '6px', border: '1px solid #d1d5db', fontSize: '0.8rem', outline: 'none' }}
                />
                <select
                    value={sort}
                    onChange={e => setSort(e.target.value as SortMode)}
                    style={{ padding: '5px 8px', borderRadius: '6px', border: '1px solid #d1d5db', fontSize: '0.8rem', outline: 'none', cursor: 'pointer' }}
                >
                    <option value="newest">{t(lang, '最新', 'Newest')}</option>
                    <option value="hottest">{t(lang, '最热', 'Hottest')}</option>
                    <option value="score">{t(lang, '评分', 'Score')}</option>
                </select>
                <button
                    onClick={() => { etagRef.current = ''; fetchSnapshot(); }}
                    style={{ padding: '5px 12px', borderRadius: '6px', border: '1px solid #d1d5db', background: '#fff', fontSize: '0.8rem', cursor: 'pointer' }}
                >
                    {t(lang, '刷新', 'Refresh')}
                </button>
            </div>

            {/* Status */}
            {loading && <div style={{ textAlign: 'center', padding: '20px', color: '#6b7280', fontSize: '0.8rem' }}>{t(lang, '加载中...', 'Loading...')}</div>}
            {error && <div style={{ textAlign: 'center', padding: '20px', color: '#ef4444', fontSize: '0.8rem' }}>{error}</div>}

            {/* Posts */}
            {!loading && !error && pageItems.length === 0 && (
                <div style={{ textAlign: 'center', padding: '40px 20px', color: '#9ca3af', fontSize: '0.85rem' }}>
                    {search ? t(lang, '没有匹配的八卦', 'No matching gossip') : t(lang, '暂无八卦', 'No gossip yet')}
                </div>
            )}

            {pageItems.map(p => (
                <div key={p.id} style={{
                    marginBottom: '10px', padding: '12px 14px', borderRadius: '10px',
                    background: '#fff', border: '1px solid #e5e7eb',
                    fontSize: '0.8rem', lineHeight: 1.6
                }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                            <span style={{ fontWeight: 600, color: '#4f46e5' }}>{p.nickname}</span>
                            <span style={{
                                padding: '1px 6px', borderRadius: '4px', fontSize: '0.65rem',
                                background: p.category === 'owner' ? '#fef3c7' : p.category === 'project' ? '#dbeafe' : '#d1fae5',
                                color: p.category === 'owner' ? '#92400e' : p.category === 'project' ? '#1e40af' : '#065f46'
                            }}>
                                {categoryLabel(lang, p.category)}
                            </span>
                            {p.locked && <span style={{ fontSize: '0.65rem', color: '#ef4444' }}>🔒</span>}
                        </div>
                        <span style={{ fontSize: '0.7rem', color: '#9ca3af' }}>
                            {new Date(p.created_at).toLocaleString()}
                        </span>
                    </div>
                    <div style={{ color: '#374151', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{p.content}</div>
                    <div style={{ display: 'flex', gap: '12px', marginTop: '6px', fontSize: '0.7rem', color: '#6b7280' }}>
                        <span>⭐ {p.score > 0 ? (p.score / Math.max(p.votes, 1)).toFixed(1) : '-'}</span>
                        <span>👥 {p.votes} {t(lang, '票', 'votes')}</span>
                    </div>
                </div>
            ))}

            {/* Pagination */}
            {totalPages > 1 && (
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: '12px', padding: '10px 0', fontSize: '0.8rem' }}>
                    <button
                        disabled={currentPage <= 1}
                        onClick={() => setPage(p => Math.max(1, p - 1))}
                        style={{ padding: '4px 10px', borderRadius: '4px', border: '1px solid #d1d5db', background: '#fff', cursor: currentPage <= 1 ? 'default' : 'pointer', opacity: currentPage <= 1 ? 0.4 : 1 }}
                    >
                        ‹ {t(lang, '上一页', 'Prev')}
                    </button>
                    <span style={{ color: '#6b7280' }}>{currentPage} / {totalPages}</span>
                    <button
                        disabled={currentPage >= totalPages}
                        onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                        style={{ padding: '4px 10px', borderRadius: '4px', border: '1px solid #d1d5db', background: '#fff', cursor: currentPage >= totalPages ? 'default' : 'pointer', opacity: currentPage >= totalPages ? 0.4 : 1 }}
                    >
                        {t(lang, '下一页', 'Next')} ›
                    </button>
                </div>
            )}

            {/* Summary */}
            <div style={{ textAlign: 'center', fontSize: '0.7rem', color: '#9ca3af', padding: '4px 0' }}>
                {t(lang, `共 ${filtered.length} 条`, `${filtered.length} total`)}
            </div>
        </div>
    );
}
