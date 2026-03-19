import { useState, useEffect, useCallback, useRef } from "react";
import { SendAIAssistantMessage, ClearAIAssistantHistory } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

export interface ChatMessage {
    id: string;
    role: 'user' | 'assistant' | 'progress' | 'error';
    content: string;
    fields?: Array<{ label: string; value: string }>;
    actions?: Array<{ label: string; command: string; style: string }>;
    localFilePath?: string;
    localFilePaths?: string[];
    thumbnailBase64?: string;
    timestamp: number;
}

// Auto-incrementing ID to avoid collisions from rapid messages / progress events.
let _nextMsgId = 1;
function nextId(): string {
    return `msg-${Date.now()}-${_nextMsgId++}`;
}

export function useAIAssistant() {
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [sending, setSending] = useState(false);
    // Ref-based guard prevents concurrent sends (React state is async).
    const sendingRef = useRef(false);

    const sendMessage = useCallback(async (text: string) => {
        if (text.trim() === "" || sendingRef.current) return;
        sendingRef.current = true;
        setSending(true);

        const userMsg: ChatMessage = {
            id: nextId(),
            role: 'user',
            content: text,
            timestamp: Date.now(),
        };
        setMessages(prev => [...prev, userMsg]);

        try {
            const response = await SendAIAssistantMessage(text);
            const assistantMsg: ChatMessage = {
                id: nextId(),
                role: response.error ? 'error' : 'assistant',
                content: response.error || response.text || '',
                fields: response.fields,
                actions: response.actions,
                localFilePath: response.local_file_path,
                localFilePaths: response.local_file_paths,
                thumbnailBase64: response.thumbnail_base64,
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, assistantMsg]);
        } catch (err: any) {
            const errorMsg: ChatMessage = {
                id: nextId(),
                role: 'error',
                content: err?.message || String(err),
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, errorMsg]);
        } finally {
            sendingRef.current = false;
            setSending(false);
        }
    }, []);

    const clearHistory = useCallback(async () => {
        try {
            await ClearAIAssistantHistory();
        } catch (_) {
            // ignore clear errors
        }
        // Reset sending state in case it was stuck (e.g. after max iterations).
        sendingRef.current = false;
        setSending(false);
        setMessages([]);
    }, []);

    const executeAction = useCallback((command: string) => {
        return sendMessage(command);
    }, [sendMessage]);

    useEffect(() => {
        const handler = (progressText: string) => {
            const progressMsg: ChatMessage = {
                id: nextId(),
                role: 'progress',
                content: progressText,
                timestamp: Date.now(),
            };
            setMessages(prev => [...prev, progressMsg]);
        };
        EventsOn("ai-assistant-progress", handler);
        return () => {
            EventsOff("ai-assistant-progress");
        };
    }, []);

    return { messages, sending, sendMessage, clearHistory, executeAction };
}
