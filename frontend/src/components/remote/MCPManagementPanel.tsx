import { useState, useEffect, useCallback } from "react";
import {
    ListMCPServers,
    RegisterMCPServer,
    UpdateMCPServer,
    UnregisterMCPServer,
    GetMCPServerTools,
    CheckMCPServerHealth,
} from "../../../wailsjs/go/main/App";

interface MCPToolView {
    name: string;
    description: string;
    input_schema: Record<string, any>;
}

interface MCPServerView {
    id: string;
    name: string;
    endpoint_url: string;
    auth_type: "none" | "api_key" | "bearer";
    auth_secret: string;
    tools: MCPToolView[];
    health_status: "healthy" | "slow" | "unavailable";
    fail_count: number;
    last_check_at: string;
    created_at: string;
}

type Props = {
    translate: (key: string) => string;
};

const emptyServer: MCPServerView = {
    id: "",
    name: "",
    endpoint_url: "",
    auth_type: "none",
    auth_secret: "",
    tools: [],
    health_status: "healthy",
    fail_count: 0,
    last_check_at: "",
    created_at: "",
};

export function MCPManagementPanel({ translate }: Props) {
    const [servers, setServers] = useState<MCPServerView[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Form state
    const [showForm, setShowForm] = useState(false);
    const [editingServer, setEditingServer] = useState<MCPServerView | null>(null);
    const [formData, setFormData] = useState<MCPServerView>({ ...emptyServer });
    const [formError, setFormError] = useState("");

    // Delete confirmation
    const [deleteTarget, setDeleteTarget] = useState<MCPServerView | null>(null);

    // Expanded tools view
    const [expandedServerID, setExpandedServerID] = useState<string | null>(null);
    const [expandedTools, setExpandedTools] = useState<MCPToolView[]>([]);
    const [toolsLoading, setToolsLoading] = useState(false);

    // Health check history (simulated from last check)
    const [healthDetailID, setHealthDetailID] = useState<string | null>(null);

    const loadData = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const list = await ListMCPServers();
            setServers(Array.isArray(list) ? list : []);
        } catch (err) {
            setError(String(err));
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        loadData();
    }, [loadData]);

    const openCreateForm = () => {
        setEditingServer(null);
        setFormData({ ...emptyServer });
        setFormError("");
        setShowForm(true);
    };

    const openEditForm = (server: MCPServerView) => {
        setEditingServer(server);
        setFormData({ ...server });
        setFormError("");
        setShowForm(true);
    };

    const closeForm = () => {
        setShowForm(false);
        setEditingServer(null);
        setFormError("");
    };

    const handleSubmit = async () => {
        if (!formData.name.trim()) {
            setFormError("名称不能为空");
            return;
        }
        if (!formData.endpoint_url.trim()) {
            setFormError("端点 URL 不能为空");
            return;
        }
        setBusy(true);
        setFormError("");
        try {
            if (editingServer) {
                await UpdateMCPServer(formData);
            } else {
                await RegisterMCPServer(formData);
            }
            closeForm();
            await loadData();
        } catch (err) {
            setFormError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const handleDelete = async (server: MCPServerView) => {
        setBusy(true);
        try {
            await UnregisterMCPServer(server.id);
            setDeleteTarget(null);
            await loadData();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const toggleTools = async (serverID: string) => {
        if (expandedServerID === serverID) {
            setExpandedServerID(null);
            setExpandedTools([]);
            return;
        }
        setExpandedServerID(serverID);
        setToolsLoading(true);
        try {
            const tools = await GetMCPServerTools(serverID);
            setExpandedTools(Array.isArray(tools) ? tools : []);
        } catch (err) {
            setExpandedTools([]);
            setError(String(err));
        } finally {
            setToolsLoading(false);
        }
    };

    const handleHealthCheck = async (serverID: string) => {
        setBusy(true);
        try {
            await CheckMCPServerHealth(serverID);
            await loadData();
        } catch (err) {
            setError(String(err));
        } finally {
            setBusy(false);
        }
    };

    const toggleHealthDetail = (serverID: string) => {
        setHealthDetailID(healthDetailID === serverID ? null : serverID);
    };

    const healthColor = (status: string): string => {
        switch (status) {
            case "healthy": return "#2f855a";
            case "slow": return "#b7791f";
            case "unavailable": return "#c53030";
            default: return "#8b95a5";
        }
    };

    const healthBg = (status: string): string => {
        switch (status) {
            case "healthy": return "#f0fdf4";
            case "slow": return "#fffbeb";
            case "unavailable": return "#fff5f5";
            default: return "#f4f5f7";
        }
    };

    const healthBorder = (status: string): string => {
        switch (status) {
            case "healthy": return "#86efac";
            case "slow": return "#fbbf24";
            case "unavailable": return "#fecdd3";
            default: return "#e1e4e8";
        }
    };

    const healthLabel = (status: string): string => {
        switch (status) {
            case "healthy": return "健康";
            case "slow": return "缓慢";
            case "unavailable": return "不可用";
            default: return status;
        }
    };

    const authLabel = (authType: string): string => {
        switch (authType) {
            case "none": return "无认证";
            case "api_key": return "API Key";
            case "bearer": return "Bearer Token";
            default: return authType;
        }
    };

    return (
        <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
            {/* Header with register button */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span style={{ fontSize: "0.78rem", color: "#5a6577" }}>
                    {servers.length} {translate("mcpServersRegistered") || "个已注册 MCP Server"}
                </span>
                <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 12px" }} onClick={openCreateForm} disabled={busy}>
                    + 注册 MCP Server
                </button>
            </div>

            {/* Loading */}
            {loading && (
                <div style={{ textAlign: "center", padding: "16px", fontSize: "0.78rem", color: "#8b95a5" }}>
                    加载中...
                </div>
            )}

            {/* Error */}
            {error && (
                <div style={{ fontSize: "0.78rem", color: "#c53030", background: "#fff5f5", padding: "6px 10px", borderRadius: "4px", border: "1px solid #fecdd3" }}>
                    {error}
                </div>
            )}

            {/* Servers table */}
            {!loading && servers.length > 0 && (
                <div style={{ border: "1px solid #e1e4e8", borderRadius: "6px", overflow: "hidden" }}>
                    <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.76rem" }}>
                        <thead>
                            <tr style={{ background: "#f4f5f7" }}>
                                <th style={thStyle}>名称</th>
                                <th style={thStyle}>端点 URL</th>
                                <th style={thStyle}>健康状态</th>
                                <th style={thStyle}>工具数</th>
                                <th style={{ ...thStyle, width: "140px" }}>操作</th>
                            </tr>
                        </thead>
                        <tbody>
                            {servers.map((s) => (
                                <ServerRow
                                    key={s.id}
                                    server={s}
                                    busy={busy}
                                    expandedServerID={expandedServerID}
                                    expandedTools={expandedTools}
                                    toolsLoading={toolsLoading}
                                    healthDetailID={healthDetailID}
                                    onEdit={() => openEditForm(s)}
                                    onDelete={() => setDeleteTarget(s)}
                                    onToggleTools={() => toggleTools(s.id)}
                                    onHealthCheck={() => handleHealthCheck(s.id)}
                                    onToggleHealthDetail={() => toggleHealthDetail(s.id)}
                                    healthColor={healthColor}
                                    healthBg={healthBg}
                                    healthBorder={healthBorder}
                                    healthLabel={healthLabel}
                                />
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {!loading && servers.length === 0 && !error && (
                <div style={{ textAlign: "center", padding: "20px", fontSize: "0.78rem", color: "#8b95a5" }}>
                    暂无已注册的 MCP Server
                </div>
            )}

            {/* Delete confirmation dialog */}
            {deleteTarget && (
                <div className="modal-backdrop" onClick={() => setDeleteTarget(null)}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "280px" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>确认删除</h3>
                            <button className="btn-close" onClick={() => setDeleteTarget(null)}>×</button>
                        </div>
                        <div className="modal-body">
                            <p style={{ fontSize: "0.8rem", color: "#5a6577", margin: 0 }}>
                                确定要注销 MCP Server「{deleteTarget.name}」吗？此操作不可撤销。
                            </p>
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={() => setDeleteTarget(null)} disabled={busy}>取消</button>
                            <button className="btn-secondary btn-danger" onClick={() => handleDelete(deleteTarget)} disabled={busy}>
                                {busy ? "删除中..." : "删除"}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Create/Edit form dialog */}
            {showForm && (
                <div className="modal-backdrop" onClick={closeForm}>
                    <div className="modal-content" onClick={(e) => e.stopPropagation()} style={{ width: "420px", textAlign: "left" }}>
                        <div className="modal-header">
                            <h3 style={{ fontSize: "0.88rem", margin: 0 }}>{editingServer ? "编辑 MCP Server" : "注册 MCP Server"}</h3>
                            <button className="btn-close" onClick={closeForm}>×</button>
                        </div>
                        <div className="modal-body" style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">名称</label>
                                <input
                                    className="form-input"
                                    value={formData.name}
                                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                                    placeholder="my-mcp-server"
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">端点 URL</label>
                                <input
                                    className="form-input"
                                    value={formData.endpoint_url}
                                    onChange={(e) => setFormData({ ...formData, endpoint_url: e.target.value })}
                                    placeholder="https://mcp.example.com/v1"
                                    spellCheck={false}
                                />
                            </div>
                            <div className="form-group" style={{ marginBottom: 0 }}>
                                <label className="form-label">认证方式</label>
                                <select
                                    className="form-input"
                                    value={formData.auth_type}
                                    onChange={(e) => setFormData({ ...formData, auth_type: e.target.value as MCPServerView["auth_type"] })}
                                >
                                    <option value="none">无认证</option>
                                    <option value="api_key">API Key</option>
                                    <option value="bearer">Bearer Token</option>
                                </select>
                            </div>
                            {formData.auth_type !== "none" && (
                                <div className="form-group" style={{ marginBottom: 0 }}>
                                    <label className="form-label">
                                        {formData.auth_type === "api_key" ? "API Key" : "Bearer Token"}
                                    </label>
                                    <input
                                        className="form-input"
                                        type="password"
                                        value={formData.auth_secret}
                                        onChange={(e) => setFormData({ ...formData, auth_secret: e.target.value })}
                                        placeholder={formData.auth_type === "api_key" ? "输入 API Key" : "输入 Bearer Token"}
                                        spellCheck={false}
                                    />
                                </div>
                            )}
                            {formError && (
                                <div style={{ fontSize: "0.76rem", color: "#c53030", background: "#fff5f5", padding: "4px 8px", borderRadius: "4px" }}>
                                    {formError}
                                </div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn-secondary" onClick={closeForm} disabled={busy}>取消</button>
                            <button className="btn-primary" style={{ fontSize: "0.78rem", padding: "4px 14px" }} onClick={handleSubmit} disabled={busy}>
                                {busy ? "提交中..." : editingServer ? "保存" : "注册"}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

/* Server row with expandable tools and health detail */
function ServerRow({
    server,
    busy,
    expandedServerID,
    expandedTools,
    toolsLoading,
    healthDetailID,
    onEdit,
    onDelete,
    onToggleTools,
    onHealthCheck,
    onToggleHealthDetail,
    healthColor,
    healthBg,
    healthBorder,
    healthLabel,
}: {
    server: MCPServerView;
    busy: boolean;
    expandedServerID: string | null;
    expandedTools: MCPToolView[];
    toolsLoading: boolean;
    healthDetailID: string | null;
    onEdit: () => void;
    onDelete: () => void;
    onToggleTools: () => void;
    onHealthCheck: () => void;
    onToggleHealthDetail: () => void;
    healthColor: (s: string) => string;
    healthBg: (s: string) => string;
    healthBorder: (s: string) => string;
    healthLabel: (s: string) => string;
}) {
    const isExpanded = expandedServerID === server.id;
    const showHealthDetail = healthDetailID === server.id;
    const toolCount = server.tools ? server.tools.length : 0;

    return (
        <>
            <tr style={{ borderTop: "1px solid #e1e4e8" }}>
                <td style={tdStyle}>{server.name}</td>
                <td style={tdStyle}>
                    <span style={{ fontFamily: "monospace", fontSize: "0.72rem", color: "#4f5d75", wordBreak: "break-all" }}>
                        {server.endpoint_url}
                    </span>
                </td>
                <td style={tdStyle}>
                    <span
                        style={{
                            ...statusBadgeStyle,
                            background: healthBg(server.health_status),
                            color: healthColor(server.health_status),
                            border: `1px solid ${healthBorder(server.health_status)}`,
                            cursor: "pointer",
                        }}
                        onClick={onToggleHealthDetail}
                        title="点击查看健康检查记录"
                    >
                        ● {healthLabel(server.health_status)}
                    </span>
                </td>
                <td style={tdStyle}>{toolCount}</td>
                <td style={tdStyle}>
                    <div style={{ display: "flex", gap: "4px", flexWrap: "wrap" }}>
                        <button className="btn-secondary" style={smallBtnStyle} onClick={onToggleTools} disabled={busy}>
                            {isExpanded ? "收起" : "工具"}
                        </button>
                        <button className="btn-secondary" style={smallBtnStyle} onClick={onEdit} disabled={busy}>编辑</button>
                        <button className="btn-secondary btn-danger" style={smallBtnStyle} onClick={onDelete} disabled={busy}>删除</button>
                    </div>
                </td>
            </tr>

            {/* Health detail row */}
            {showHealthDetail && (
                <tr>
                    <td colSpan={5} style={{ padding: "6px 8px", background: "#fafbfc", borderTop: "1px solid #e1e4e8" }}>
                        <div style={{ fontSize: "0.72rem", color: "#5a6577" }}>
                            <div style={{ fontWeight: 600, marginBottom: "4px" }}>健康检查记录</div>
                            <div style={{ display: "flex", gap: "6px", alignItems: "center", flexWrap: "wrap" }}>
                                <span>状态: <span style={{ color: healthColor(server.health_status), fontWeight: 600 }}>{healthLabel(server.health_status)}</span></span>
                                <span>·</span>
                                <span>失败次数: {server.fail_count}</span>
                                <span>·</span>
                                <span>最近检查: {server.last_check_at ? new Date(server.last_check_at).toLocaleString() : "—"}</span>
                                <button className="btn-secondary" style={{ ...smallBtnStyle, marginLeft: "8px" }} onClick={onHealthCheck} disabled={busy}>
                                    立即检查
                                </button>
                            </div>
                        </div>
                    </td>
                </tr>
            )}

            {/* Expanded tools row */}
            {isExpanded && (
                <tr>
                    <td colSpan={5} style={{ padding: "6px 8px", background: "#fafbfc", borderTop: "1px solid #e1e4e8" }}>
                        {toolsLoading ? (
                            <div style={{ fontSize: "0.74rem", color: "#8b95a5", padding: "4px 0" }}>加载工具列表...</div>
                        ) : expandedTools.length > 0 ? (
                            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                                <div style={{ fontSize: "0.72rem", fontWeight: 600, color: "#5a6577", marginBottom: "2px" }}>
                                    工具列表 ({expandedTools.length})
                                </div>
                                {expandedTools.map((tool) => (
                                    <div key={tool.name} style={{ background: "#ffffff", border: "1px solid #e1e4e8", borderRadius: "4px", padding: "4px 8px" }}>
                                        <div style={{ fontSize: "0.74rem", fontWeight: 600, color: "#1a202c" }}>{tool.name}</div>
                                        <div style={{ fontSize: "0.7rem", color: "#5a6577" }}>{tool.description || "无描述"}</div>
                                    </div>
                                ))}
                            </div>
                        ) : (
                            <div style={{ fontSize: "0.74rem", color: "#8b95a5", padding: "4px 0" }}>暂无工具</div>
                        )}
                    </td>
                </tr>
            )}
        </>
    );
}

/* Inline style constants */
const thStyle: React.CSSProperties = {
    padding: "6px 8px",
    textAlign: "left",
    fontWeight: 600,
    fontSize: "0.74rem",
    color: "#5a6577",
    borderBottom: "1px solid #e1e4e8",
};

const tdStyle: React.CSSProperties = {
    padding: "6px 8px",
    fontSize: "0.76rem",
    color: "#1a202c",
    verticalAlign: "top",
};

const statusBadgeStyle: React.CSSProperties = {
    display: "inline-block",
    padding: "1px 8px",
    borderRadius: "999px",
    fontSize: "0.68rem",
    fontWeight: 600,
};

const smallBtnStyle: React.CSSProperties = {
    fontSize: "0.72rem",
    padding: "2px 8px",
};
