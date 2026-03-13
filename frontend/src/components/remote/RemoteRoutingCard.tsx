import { main } from "../../../wailsjs/go/models";
import type { RemoteSuggestedAction, RemoteToolMetadataView, RemoteToolName } from "./types";
import { remoteCardStyle, remoteLabelStyle, remoteMetaLabelStyle, remoteMutedCardStyle, remoteSectionTitleStyle } from "./styles";

type Props = {
    config: main.AppConfig | null;
    remoteBusy: string;
    selectedRemoteTool: RemoteToolName;
    visibleRemoteTools: RemoteToolMetadataView[];
    selectedRemoteToolInfo?: RemoteToolMetadataView;
    selectedRemoteToolCanStart: boolean;
    selectedRemoteToolUnavailableReason: string;
    selectedRemoteToolBadges: string[];
    remoteSuggestedAction: RemoteSuggestedAction | null;
    getRemoteToolLabel: (tool: string) => string;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    setSelectedRemoteTool: (tool: RemoteToolName) => void;
    activateRemoteWithEmail: () => void;
    startRemoteSession: () => void;
    installSelectedRemoteTool: () => void;
    reconnectRemote: () => void;
    clearRemoteActivationState: () => void;
    refreshRemoteReadiness: () => void;
    switchTool: (tool: string) => void;
    onDemandInstallingTool: string;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
};

export function RemoteRoutingCard(props: Props) {
    const {
        config,
        remoteBusy,
        selectedRemoteTool,
        visibleRemoteTools,
        selectedRemoteToolInfo,
        selectedRemoteToolCanStart,
        selectedRemoteToolUnavailableReason,
        selectedRemoteToolBadges,
        remoteSuggestedAction,
        getRemoteToolLabel,
        saveRemoteConfigField,
        setSelectedRemoteTool,
        activateRemoteWithEmail,
        startRemoteSession,
        installSelectedRemoteTool,
        reconnectRemote,
        clearRemoteActivationState,
        refreshRemoteReadiness,
        switchTool,
        onDemandInstallingTool,
        translate,
        formatText,
    } = props;

    return (
        <div style={remoteCardStyle}>
            <div style={remoteSectionTitleStyle}>{translate("remoteRouting")}</div>

            <label style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "12px", cursor: "pointer", padding: "10px 12px", borderRadius: "12px", background: "#f8fafc", border: "1px solid #e2e8f0" }}>
                <input
                    type="checkbox"
                    checked={!!config?.remote_enabled}
                    onChange={(e) => saveRemoteConfigField({ remote_enabled: e.target.checked })}
                />
                <span style={{ fontSize: "0.8rem", color: "#475569", fontWeight: 600 }}>{translate("remoteEnableLaunchPath")}</span>
            </label>

            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: "10px" }}>
                <div>
                    <div style={remoteLabelStyle}>{translate("remoteHubUrl")}</div>
                    <input className="form-input" value={config?.remote_hub_url || ""} onChange={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value })} onBlur={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value.trim() })} placeholder="https://hub.example.com" />
                </div>
                <div>
                    <div style={remoteLabelStyle}>{translate("remoteHubCenterUrl")}</div>
                    <input className="form-input" value={config?.remote_hubcenter_url || ""} onChange={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value })} onBlur={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value.trim() })} placeholder="http://127.0.0.1:9388" />
                </div>
                <div style={{ gridColumn: "1 / -1" }}>
                    <div style={remoteLabelStyle}>{translate("remoteEmail")}</div>
                    <input className="form-input" value={config?.remote_email || ""} onChange={(e) => saveRemoteConfigField({ remote_email: e.target.value })} onBlur={(e) => saveRemoteConfigField({ remote_email: e.target.value.trim() })} placeholder="name@example.com" />
                </div>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "minmax(180px, 220px) 1fr", gap: "10px", marginTop: "12px", alignItems: "start" }}>
                <div>
                    <div style={remoteLabelStyle}>{translate("remoteTool")}</div>
                    <select className="form-input" value={selectedRemoteTool} onChange={(e) => setSelectedRemoteTool(e.target.value as RemoteToolName)} disabled={!!remoteBusy}>
                        {visibleRemoteTools.map((tool) => (
                            <option key={tool.name} value={tool.name} disabled={tool.can_start === false}>
                                {tool.display_name}{tool.installed === false ? ` (${translate("remoteNotInstalled")})` : ""}
                            </option>
                        ))}
                    </select>
                </div>
                <div style={{ display: "flex", gap: "8px", flexWrap: "wrap" }}>
                    <button className="btn-primary" disabled={!!remoteBusy} onClick={activateRemoteWithEmail}>
                        {remoteBusy === "activate" ? translate("remoteActivating") : translate("remoteActivate")}
                    </button>
                    <button className="btn-primary" disabled={!!remoteBusy || !selectedRemoteToolCanStart} onClick={startRemoteSession}>
                        {remoteBusy === "start-session"
                            ? translate("remoteStarting")
                            : (selectedRemoteToolCanStart
                                ? formatText("remoteStartTool", { tool: getRemoteToolLabel(selectedRemoteTool) })
                                : formatText("remoteUnavailable", { reason: selectedRemoteToolUnavailableReason }))}
                    </button>
                    {!selectedRemoteToolInfo?.installed && (
                        <button className="btn-secondary" disabled={!!remoteBusy || onDemandInstallingTool === selectedRemoteTool} onClick={installSelectedRemoteTool}>
                            {remoteBusy === "install-remote-tool"
                                ? formatText("remoteInstallingTool", { tool: getRemoteToolLabel(selectedRemoteTool) })
                                : formatText("remoteInstallTool", { tool: getRemoteToolLabel(selectedRemoteTool) })}
                        </button>
                    )}
                    <button className="btn-secondary" disabled={!!remoteBusy} onClick={reconnectRemote}>
                        {remoteBusy === "reconnect" ? translate("remoteReconnecting") : translate("remoteReconnectHub")}
                    </button>
                    <button className="btn-secondary" disabled={!!remoteBusy} onClick={clearRemoteActivationState}>
                        {remoteBusy === "clear" ? translate("remoteClearing") : translate("remoteClearActivation")}
                    </button>
                </div>
            </div>

            <div style={{ marginTop: "12px", display: "flex", flexDirection: "column", gap: "8px" }}>
                <div style={{ display: "flex", gap: "6px", flexWrap: "wrap" }}>
                    {selectedRemoteToolBadges.map((badge) => (
                        <span key={badge} style={{ fontSize: "0.68rem", fontWeight: 700, padding: "3px 8px", borderRadius: "999px", background: "#eef2ff", color: "#3730a3", border: "1px solid #c7d2fe" }}>{badge}</span>
                    ))}
                </div>
                <div style={{ fontSize: "0.74rem", color: "#64748b", wordBreak: "break-word" }}>
                    {translate("remoteToolPath")}: <span style={{ color: "#334155", fontWeight: 600 }}>{selectedRemoteToolInfo?.tool_path || translate("remoteNotInstalled")}</span>
                </div>
                {!selectedRemoteToolCanStart && <div style={{ fontSize: "0.74rem", color: "#b45309" }}>{formatText("remoteUnavailable", { reason: selectedRemoteToolUnavailableReason })}</div>}
                {remoteSuggestedAction && (
                    <div style={{ ...remoteMutedCardStyle, display: "flex", alignItems: "center", justifyContent: "space-between", gap: "10px", flexWrap: "wrap" }}>
                        <div style={{ minWidth: 0 }}>
                            <div style={{ ...remoteMetaLabelStyle, marginBottom: "4px" }}>{translate("remoteNextStep")}</div>
                            <div style={{ fontSize: "0.8rem", fontWeight: 700, color: "#334155", marginBottom: "2px" }}>{remoteSuggestedAction.label}</div>
                            <div style={{ fontSize: "0.74rem", color: "#64748b", wordBreak: "break-word" }}>{remoteSuggestedAction.description}</div>
                        </div>
                        <button
                            className="btn-primary"
                            disabled={!!remoteBusy}
                            onClick={() => {
                                if (remoteSuggestedAction.action === "install") installSelectedRemoteTool();
                                if (remoteSuggestedAction.action === "activate") activateRemoteWithEmail();
                                if (remoteSuggestedAction.action === "configure") switchTool(selectedRemoteTool);
                                if (remoteSuggestedAction.action === "reconnect") reconnectRemote();
                                if (remoteSuggestedAction.action === "readiness") refreshRemoteReadiness();
                                if (remoteSuggestedAction.action === "start") startRemoteSession();
                            }}
                        >
                            {remoteSuggestedAction.label}
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}
