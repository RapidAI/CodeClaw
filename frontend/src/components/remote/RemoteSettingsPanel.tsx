import { main } from "../../../wailsjs/go/models";
import type { RemoteActivationStatus } from "./types";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    translate: (key: string) => string;
    remoteBusy: string;
    remoteActivationStatus: RemoteActivationStatus | null;
    remoteSmokeReport: any;
    getRemoteSmokeDetail: () => string;
    activateRemoteWithEmail: () => void;
};

export function RemoteSettingsPanel({
    config,
    saveRemoteConfigField,
    translate,
    remoteBusy,
    remoteActivationStatus,
    remoteSmokeReport,
    getRemoteSmokeDetail,
    activateRemoteWithEmail,
}: Props) {
    return (
        <div className="settings-panel">
            <div className="settings-panel-header">
                <div>
                    <h3 className="settings-panel-title">{translate("remoteControl")}</h3>
                    <p className="settings-panel-desc">
                        {translate("remoteControlDesc")}
                    </p>
                </div>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: "14px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hub_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value.trim() })}
                        placeholder="https://hub.example.com"
                        spellCheck={false}
                    />
                </div>

                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubCenterUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hubcenter_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value.trim() })}
                        placeholder="http://127.0.0.1:9388"
                        spellCheck={false}
                    />
                </div>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "minmax(260px, 1fr) auto", gap: "14px", marginTop: "14px", alignItems: "end" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteEmail")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_email || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_email: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_email: e.target.value.trim() })}
                        placeholder="name@example.com"
                        spellCheck={false}
                    />
                </div>
                <button className="btn-primary" disabled={!!remoteBusy} onClick={activateRemoteWithEmail} style={{ minWidth: "140px" }}>
                    {remoteBusy === "activate" ? translate("remoteActivating") : translate("remoteActivate")}
                </button>
            </div>

            <div className="info-text" style={{ marginTop: "14px", textAlign: "left" }}>
                {remoteActivationStatus?.activated
                    ? `${translate("remoteActivation")}: ${translate("remoteActivated")} ${remoteActivationStatus.email ? `(${remoteActivationStatus.email})` : ""}`
                    : `${translate("remoteActivation")}: ${translate("remoteNotActivated")}`}
            </div>

            <div className="info-text" style={{ marginTop: "10px", textAlign: "left" }}>
                {translate("remoteModeDesc")}
            </div>

            <div
                style={{
                    marginTop: "16px",
                    border: "1px solid rgba(15, 23, 42, 0.12)",
                    borderRadius: "14px",
                    padding: "14px 16px",
                    background: "rgba(248, 250, 252, 0.92)",
                }}
            >
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "12px" }}>
                    <div>
                        <div style={{ fontWeight: 700, color: "#0f172a" }}>Latest Full Demo</div>
                        <div style={{ fontSize: "12px", color: "#64748b", marginTop: "4px" }}>
                            {remoteSmokeReport?.last_updated || "No recorded full demo yet"}
                        </div>
                    </div>
                    <div
                        style={{
                            padding: "4px 10px",
                            borderRadius: "999px",
                            fontSize: "12px",
                            fontWeight: 700,
                            color: remoteSmokeReport?.success ? "#166534" : "#9f1239",
                            background: remoteSmokeReport?.success ? "rgba(34,197,94,0.12)" : "rgba(244,63,94,0.12)",
                        }}
                    >
                        {remoteSmokeReport ? (remoteSmokeReport.success ? "Success" : "Needs Attention") : "Not Run"}
                    </div>
                </div>

                <div style={{ marginTop: "12px", fontSize: "13px", color: "#334155", lineHeight: 1.6 }}>
                    <div><strong>Phase:</strong> {remoteSmokeReport?.phase || "idle"}</div>
                    <div><strong>Summary:</strong> {getRemoteSmokeDetail()}</div>
                    {remoteSmokeReport?.recommended_next ? (
                        <div><strong>Next:</strong> {remoteSmokeReport.recommended_next}</div>
                    ) : null}
                    {remoteSmokeReport?.started_session?.id ? (
                        <div><strong>Session:</strong> {remoteSmokeReport.started_session.id}</div>
                    ) : null}
                    {remoteSmokeReport?.hub_visibility ? (
                        <div><strong>Hub Visible:</strong> {remoteSmokeReport.hub_visibility.verified ? "Yes" : "No"}</div>
                    ) : null}
                </div>
            </div>
        </div>
    );
}
