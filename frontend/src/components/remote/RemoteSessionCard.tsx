import type { Dispatch, SetStateAction } from "react";
import { remoteMetaLabelStyle, remoteSessionCardStyle } from "./styles";
import type { RemoteSessionView } from "./types";

type Props = {
    session: RemoteSessionView;
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => void;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
};

export function RemoteSessionCard(props: Props) {
    const {
        session,
        remoteInputDrafts,
        setRemoteInputDrafts,
        sendRemoteInput,
        interruptRemoteSession,
        killRemoteSession,
        showToastMessage,
        translate,
        formatText,
    } = props;

    return (
        <div style={remoteSessionCardStyle}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "start", gap: "12px", marginBottom: "10px" }}>
                <div style={{ minWidth: 0 }}>
                    <div style={{ fontSize: "0.85rem", fontWeight: 700, color: "#0f172a", marginBottom: "4px" }}>{session.title || session.project_path || session.id}</div>
                    <div style={{ fontSize: "0.75rem", color: "#64748b", wordBreak: "break-word" }}>{session.project_path || session.id}</div>
                </div>
                <span style={{ fontSize: "0.72rem", fontWeight: 700, padding: "4px 8px", borderRadius: "999px", background: session.status === "error" ? "#fee2e2" : session.status === "waiting_input" ? "#fef3c7" : "#dbeafe", color: session.status === "error" ? "#dc2626" : session.status === "waiting_input" ? "#d97706" : "#2563eb", textTransform: "capitalize", flexShrink: 0 }}>
                    {session.status || translate("remoteStatusUnknown")}
                </span>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: "8px", marginBottom: "10px" }}>
                <div><div style={{ ...remoteMetaLabelStyle, marginBottom: "4px" }}>{translate("remoteCurrentTask")}</div><div style={{ fontSize: "0.78rem", color: "#334155" }}>{session.summary?.current_task || "-"}</div></div>
                <div><div style={{ ...remoteMetaLabelStyle, marginBottom: "4px" }}>{translate("remoteLastResult")}</div><div style={{ fontSize: "0.78rem", color: "#334155" }}>{session.summary?.last_result || "-"}</div></div>
                <div><div style={{ ...remoteMetaLabelStyle, marginBottom: "4px" }}>{translate("remoteProgress")}</div><div style={{ fontSize: "0.78rem", color: "#334155" }}>{session.summary?.progress_summary || "-"}</div></div>
            </div>

            <div style={{ marginBottom: "10px" }}>
                <div style={{ ...remoteMetaLabelStyle, marginBottom: "6px" }}>{translate("remoteRecentActivity")}</div>
                {session.events && session.events.length > 0 ? (
                    <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
                        {session.events.slice(-3).reverse().map((event, index) => (
                            <div key={`${session.id}-event-${index}`} style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: "10px", padding: "8px 10px", borderRadius: "10px", background: "#ffffff", border: "1px solid #e2e8f0" }}>
                                <div style={{ minWidth: 0 }}>
                                    <div style={{ fontSize: "0.76rem", fontWeight: 600, color: "#334155", display: "flex", alignItems: "center", gap: "6px", flexWrap: "wrap" }}>
                                        <span>{event.title || event.type || translate("remoteEvent")}</span>
                                        {event.grouped && (event.count || 0) > 1 ? <span style={{ fontSize: "0.65rem", fontWeight: 700, padding: "2px 6px", borderRadius: "999px", background: "#dbeafe", color: "#1d4ed8" }}>x{event.count}</span> : null}
                                    </div>
                                    <div style={{ fontSize: "0.74rem", color: "#64748b", marginTop: "2px", wordBreak: "break-word" }}>{event.summary || "-"}</div>
                                </div>
                                <span style={{ fontSize: "0.65rem", fontWeight: 700, padding: "2px 6px", borderRadius: "999px", background: event.severity === "error" ? "#fee2e2" : event.severity === "warn" ? "#fef3c7" : "#ecfeff", color: event.severity === "error" ? "#b91c1c" : event.severity === "warn" ? "#b45309" : "#0f766e", textTransform: "uppercase", flexShrink: 0 }}>
                                    {event.severity || translate("remoteSeverityInfo")}
                                </span>
                            </div>
                        ))}
                    </div>
                ) : <div style={{ fontSize: "0.78rem", color: "#94a3b8" }}>{translate("remoteNoImportantEvents")}</div>}
            </div>

            <div style={{ display: "flex", gap: "8px", alignItems: "center", flexWrap: "wrap" }}>
                <input
                    className="form-input"
                    style={{ flex: "1 1 320px", minWidth: "200px" }}
                    value={remoteInputDrafts[session.id] || ""}
                    onChange={(e) => setRemoteInputDrafts((prev) => ({ ...prev, [session.id]: e.target.value }))}
                    placeholder={translate("remoteSendInstructionPlaceholder")}
                />
                <button className="btn-primary" onClick={() => sendRemoteInput(session.id)}>{translate("remoteSend")}</button>
                <button className="btn-secondary" onClick={async () => {
                    try {
                        await interruptRemoteSession(session.id);
                        showToastMessage(translate("remoteInterruptSent"), 2500);
                    } catch (err) {
                        showToastMessage(formatText("remoteInterruptFailed", { error: String(err) }), 4000);
                    }
                }}>{translate("remoteInterrupt")}</button>
                <button className="btn-secondary" style={{ background: "#fff1f2", color: "#be123c", borderColor: "#fecdd3" }} onClick={async () => {
                    try {
                        await killRemoteSession(session.id);
                        showToastMessage(translate("remoteKillSent"), 2500);
                    } catch (err) {
                        showToastMessage(formatText("remoteKillFailed", { error: String(err) }), 4000);
                    }
                }}>{translate("remoteKillSession")}</button>
            </div>
        </div>
    );
}
