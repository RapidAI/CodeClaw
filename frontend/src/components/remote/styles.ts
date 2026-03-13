import type { CSSProperties } from "react";

export const remoteCardStyle: CSSProperties = {
    border: "1px solid #e5e7eb",
    borderRadius: "12px",
    padding: "12px",
    background: "#fff",
};

export const remoteMutedCardStyle: CSSProperties = {
    padding: "10px",
    borderRadius: "10px",
    background: "#f8fafc",
    border: "1px solid #e2e8f0",
};

export const remoteSessionCardStyle: CSSProperties = {
    border: "1px solid #e2e8f0",
    borderRadius: "12px",
    padding: "12px",
    background: "#f8fafc",
};

export const remotePanelGridStyle: CSSProperties = {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
    gap: "12px",
    marginBottom: "14px",
};

export const remoteSectionTitleStyle: CSSProperties = {
    fontSize: "0.78rem",
    fontWeight: 700,
    color: "#334155",
    marginBottom: "10px",
};

export const remoteLabelStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#64748b",
    marginBottom: "4px",
};

export const remoteMetaLabelStyle: CSSProperties = {
    fontSize: "0.72rem",
    color: "#94a3b8",
    textTransform: "uppercase",
    letterSpacing: "0.04em",
    marginBottom: "8px",
};

export const remoteBodyTextStyle: CSSProperties = {
    fontSize: "0.75rem",
    color: "#64748b",
};

export const remoteActionButtonStyle: CSSProperties = {
    fontSize: "0.75rem",
    padding: "4px 10px",
};

export const remoteToolbarCardStyle: CSSProperties = {
    border: "1px solid #dbeafe",
    borderRadius: "16px",
    padding: "14px 16px",
    background: "linear-gradient(180deg, #f8fbff 0%, #eff6ff 100%)",
    boxShadow: "0 8px 20px rgba(59, 130, 246, 0.08)",
};
