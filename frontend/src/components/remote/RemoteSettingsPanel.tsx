import { useState, useCallback } from "react";
import { main } from "../../../wailsjs/go/models";
import { ListRemoteHubs } from "../../../wailsjs/go/main/App";
import type { RemoteActivationStatus } from "./types";

interface HubOption {
    hub_id: string;
    name: string;
    base_url: string;
}

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    translate: (key: string) => string;
    remoteBusy: string;
    remoteActivationStatus: RemoteActivationStatus | null;
    activateRemoteWithEmail: () => void;
    invitationCodeRequired: boolean;
    invitationCode: string;
    setInvitationCode: (code: string) => void;
    invitationCodeError: string;
};

export function RemoteSettingsPanel({
    config,
    saveRemoteConfigField,
    translate,
    remoteBusy,
    remoteActivationStatus,
    activateRemoteWithEmail,
    invitationCodeRequired,
    invitationCode,
    setInvitationCode,
    invitationCodeError,
}: Props) {
    const [hubList, setHubList] = useState<HubOption[]>([]);
    const [loadingHubs, setLoadingHubs] = useState(false);
    const [hubProbeError, setHubProbeError] = useState("");
    const [showMobileConfirm, setShowMobileConfirm] = useState(false);
    const [showNoMobilePrompt, setShowNoMobilePrompt] = useState(false);

    const probeHubs = async () => {
        const centerURL = (config?.remote_hubcenter_url || "").trim();
        const email = (config?.remote_email || "").trim();
        if (!email) {
            setHubProbeError("请先填写绑定邮件");
            return;
        }
        setLoadingHubs(true);
        setHubProbeError("");
        setHubList([]);
        try {
            const hubs = await ListRemoteHubs(centerURL, email) as HubOption[];
            setHubList(Array.isArray(hubs) ? hubs : []);
            if (!hubs || hubs.length === 0) {
                setHubProbeError("未发现可用的 Hub");
            }
        } catch (err) {
            setHubProbeError(String(err));
            setHubList([]);
        } finally {
            setLoadingHubs(false);
        }
    };

    const handleHubSelect = (value: string) => {
        if (value) {
            saveRemoteConfigField({ remote_hub_url: value });
        }
    };

    const handleRegisterClick = useCallback(() => {
        const mobile = ((config as any)?.remote_mobile || "").trim();
        if (mobile) {
            setShowMobileConfirm(true);
        } else {
            setShowNoMobilePrompt(true);
        }
    }, [config]);

    const confirmAndRegister = useCallback(() => {
        setShowMobileConfirm(false);
        activateRemoteWithEmail();
    }, [activateRemoteWithEmail]);

    const skipFeishuAndRegister = useCallback(() => {
        setShowNoMobilePrompt(false);
        activateRemoteWithEmail();
    }, [activateRemoteWithEmail]);

    return (
        <>
            {/* Row 1: Hub Center + 心跳间隔 */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 160px", gap: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubCenterUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hubcenter_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value.trim() })}
                        placeholder="http://hubs.mypapers.top:9388"
                        spellCheck={false}
                    />
                </div>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">心跳间隔（秒）</label>
                    <input
                        className="form-input"
                        type="number"
                        min={5}
                        step={1}
                        value={config?.remote_heartbeat_sec || 10}
                        onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || 10) })}
                        onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(5, Number(e.target.value || 10)) })}
                    />
                </div>
            </div>

            {/* Row 2: Hub 探测 + 选择/手工输入 */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr auto", gap: "10px", alignItems: "end", marginTop: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "6px" }}>
                        <label className="form-label" style={{ marginBottom: 0 }}>Hub 地址</label>
                        <button
                            className="btn-secondary"
                            onClick={probeHubs}
                            disabled={loadingHubs}
                            style={{ minWidth: "90px", height: "26px", padding: "2px 10px", fontSize: "0.78rem", flexShrink: 0 }}
                        >
                            {loadingHubs ? "探测中..." : "探测可用 Hub"}
                        </button>
                    </div>
                    {hubList.length > 0 && (
                        <select
                            className="form-select"
                            value={hubList.some((h) => h.base_url === (config?.remote_hub_url || "").trim()) ? (config?.remote_hub_url || "").trim() : ""}
                            onChange={(e) => handleHubSelect(e.target.value)}
                            style={{ marginBottom: "6px" }}
                        >
                            <option value="">-- 从列表中选择 --</option>
                            {hubList.map((hub) => (
                                <option key={`${hub.hub_id}-${hub.base_url}`} value={hub.base_url}>
                                    {hub.name ? `${hub.name} (${hub.base_url})` : hub.base_url}
                                </option>
                            ))}
                        </select>
                    )}
                    <input
                        className="form-input"
                        value={config?.remote_hub_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value.trim() })}
                        placeholder="https://hub.example.com（可从上方列表选择或手工输入）"
                        spellCheck={false}
                    />
                    {hubProbeError && (
                        <div style={{ fontSize: "0.78rem", color: "#ef4444", marginTop: "4px" }}>{hubProbeError}</div>
                    )}
                </div>
            </div>

            {/* Row 3: 邮件 + 手机号(仅首次注册) + 邀请码 */}
            {!remoteActivationStatus?.activated ? (
                <div style={{ display: "grid", gridTemplateColumns: invitationCodeRequired ? "1fr 1fr 1fr" : "1fr 1fr", gap: "10px", marginTop: "10px" }}>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                        <label className="form-label">{translate("remoteBindEmail")}</label>
                        <input
                            className="form-input"
                            value={config?.remote_email || ""}
                            onChange={(e) => saveRemoteConfigField({ remote_email: e.target.value })}
                            onBlur={(e) => saveRemoteConfigField({ remote_email: e.target.value.trim() })}
                            placeholder="name@example.com"
                            spellCheck={false}
                        />
                    </div>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                        <label className="form-label">手机号（飞书自动加入组织）</label>
                        <input
                            className="form-input"
                            value={(config as any)?.remote_mobile || ""}
                            onChange={(e) => saveRemoteConfigField({ remote_mobile: e.target.value } as any)}
                            onBlur={(e) => saveRemoteConfigField({ remote_mobile: e.target.value.trim() } as any)}
                            placeholder="+86 13800138000"
                            spellCheck={false}
                        />
                    </div>
                    {invitationCodeRequired && (
                        <div className="form-group" style={{ marginBottom: 0 }}>
                            <label className="form-label">邀请码</label>
                            <input
                                className="form-input"
                                value={invitationCode}
                                onChange={(e) => setInvitationCode(e.target.value.toUpperCase())}
                                placeholder="请输入邀请码"
                                spellCheck={false}
                                maxLength={10}
                                style={invitationCodeError ? { borderColor: "#ef4444" } : undefined}
                            />
                            {invitationCodeError && (
                                <div style={{ fontSize: "0.78rem", color: "#ef4444", marginTop: "4px" }}>{invitationCodeError}</div>
                            )}
                        </div>
                    )}
                </div>
            ) : (
                <div style={{ marginTop: "10px" }}>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                        <label className="form-label">{translate("remoteBindEmail")}</label>
                        <input
                            className="form-input"
                            value={config?.remote_email || ""}
                            disabled
                            spellCheck={false}
                        />
                    </div>
                </div>
            )}

            {/* Row 4: 注册按钮（仅首次注册时显示） */}
            {!remoteActivationStatus?.activated && (
                <div style={{ marginTop: "10px" }}>
                    <button className="btn-primary remote-activation-button" style={{ width: "100%" }} disabled={!!remoteBusy} onClick={handleRegisterClick}>
                        {remoteBusy === "activate" ? "注册中..." : "注册"}
                    </button>
                </div>
            )}

            {/* 手机号确认弹窗 */}
            {showMobileConfirm && (
                <div
                    style={{
                        position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                        background: "rgba(0,0,0,0.35)", display: "flex",
                        alignItems: "center", justifyContent: "center", zIndex: 9999,
                    }}
                    onClick={() => setShowMobileConfirm(false)}
                >
                    <div
                        style={{
                            background: "#fff", borderRadius: "16px", padding: "24px 28px",
                            maxWidth: "420px", width: "90%", boxShadow: "0 16px 40px rgba(0,0,0,0.18)",
                        }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div style={{ fontSize: "16px", fontWeight: 700, marginBottom: "12px" }}>
                            确认手机号
                        </div>
                        <div style={{ fontSize: "14px", color: "#555", lineHeight: 1.6, marginBottom: "8px" }}>
                            注册后将使用以下手机号自动加入飞书组织。
                            <br />
                            手机号填写错误会导致邀请失败，且需要管理员手动处理，请务必确认：
                        </div>
                        <div style={{
                            fontSize: "20px", fontWeight: 700, textAlign: "center",
                            padding: "14px", margin: "12px 0", borderRadius: "10px",
                            background: "#f0f5ff", color: "#1a3a6b", letterSpacing: "1px",
                        }}>
                            {((config as any)?.remote_mobile || "").trim()}
                        </div>
                        <div style={{ display: "flex", gap: "10px", justifyContent: "flex-end", marginTop: "16px" }}>
                            <button
                                className="btn-secondary"
                                style={{ minWidth: "80px" }}
                                onClick={() => setShowMobileConfirm(false)}
                            >
                                返回修改
                            </button>
                            <button
                                className="btn-primary"
                                style={{ minWidth: "80px" }}
                                onClick={confirmAndRegister}
                            >
                                确认注册
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* 未填手机号提示弹窗 */}
            {showNoMobilePrompt && (
                <div
                    style={{
                        position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
                        background: "rgba(0,0,0,0.35)", display: "flex",
                        alignItems: "center", justifyContent: "center", zIndex: 9999,
                    }}
                    onClick={() => setShowNoMobilePrompt(false)}
                >
                    <div
                        style={{
                            background: "#fff", borderRadius: "16px", padding: "24px 28px",
                            maxWidth: "420px", width: "90%", boxShadow: "0 16px 40px rgba(0,0,0,0.18)",
                        }}
                        onClick={(e) => e.stopPropagation()}
                    >
                        <div style={{ fontSize: "16px", fontWeight: 700, marginBottom: "12px" }}>
                            是否需要使用飞书？
                        </div>
                        <div style={{ fontSize: "14px", color: "#555", lineHeight: 1.7, marginBottom: "4px" }}>
                            您尚未填写手机号。如果需要通过飞书接收消息和管理会话，请先填写手机号再注册，系统会自动将您加入飞书组织。
                        </div>
                        <div style={{ fontSize: "13px", color: "#888", lineHeight: 1.6, marginBottom: "12px" }}>
                            如果确定不使用飞书，可以直接注册，后续将无法通过飞书进行交互。
                        </div>
                        <div style={{ display: "flex", gap: "10px", justifyContent: "flex-end", marginTop: "16px" }}>
                            <button
                                className="btn-ghost"
                                style={{ minWidth: "120px", color: "#999", fontSize: "0.85rem" }}
                                onClick={skipFeishuAndRegister}
                            >
                                不使用飞书，直接注册
                            </button>
                            <button
                                className="btn-primary"
                                style={{ minWidth: "100px" }}
                                onClick={() => setShowNoMobilePrompt(false)}
                            >
                                去填写手机号
                            </button>
                        </div>
                    </div>
                </div>
            )}

            <div className="info-text" style={{ marginTop: "10px", textAlign: "left" }}>
                {remoteActivationStatus?.activated
                    ? `${translate("remoteActivation")}: ${translate("remoteActivated")} ${remoteActivationStatus.email ? `(${remoteActivationStatus.email})` : ""}`
                    : `${translate("remoteActivation")}: ${translate("remoteNotActivated")}`}{" | "}{translate("remoteModeDesc")}
            </div>
        </>
    );
}
