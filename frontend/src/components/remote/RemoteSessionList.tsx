import type { Dispatch, SetStateAction } from "react";
import { RemoteSessionCard } from "./RemoteSessionCard";
import { remoteBodyTextStyle, remoteCardStyle, remoteSectionTitleStyle } from "./styles";
import type { RemoteSessionView } from "./types";

type Props = {
    remoteSessions: RemoteSessionView[];
    remoteInputDrafts: Record<string, string>;
    setRemoteInputDrafts: Dispatch<SetStateAction<Record<string, string>>>;
    sendRemoteInput: (sessionID: string) => void;
    interruptRemoteSession: (sessionID: string) => Promise<void>;
    killRemoteSession: (sessionID: string) => Promise<void>;
    showToastMessage: (message: string, duration?: number) => void;
    translate: (key: string) => string;
    formatText: (key: string, values?: Record<string, string>) => string;
};

export function RemoteSessionList(props: Props) {
    const {
        remoteSessions,
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
        <div style={{ ...remoteCardStyle, marginBottom: "14px" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "12px", marginBottom: "12px", flexWrap: "wrap" }}>
                <div>
                    <div style={{ ...remoteSectionTitleStyle, marginBottom: 0 }}>{translate("remoteManagedSessions")}</div>
                    <div style={{ fontSize: "0.74rem", color: "#64748b" }}>{translate("remoteManagedSessionsDesc")}</div>
                </div>
                <div style={remoteBodyTextStyle}>{formatText("remoteActiveRecords", { count: String(remoteSessions.length) })}</div>
            </div>
            {remoteSessions.length === 0 ? (
                <div style={{ fontSize: "0.78rem", color: "#94a3b8", padding: "8px 0" }}>{translate("remoteNoSessions")}</div>
            ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
                    {remoteSessions.map((session) => (
                        <RemoteSessionCard
                            key={session.id}
                            session={session}
                            remoteInputDrafts={remoteInputDrafts}
                            setRemoteInputDrafts={setRemoteInputDrafts}
                            sendRemoteInput={sendRemoteInput}
                            interruptRemoteSession={interruptRemoteSession}
                            killRemoteSession={killRemoteSession}
                            showToastMessage={showToastMessage}
                            translate={translate}
                            formatText={formatText}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}
