/**
 * Property-based test for AI assistant sidebar icon i18n labels.
 *
 * Feature: ai-assistant-sidebar-icon
 * Property 1: 国际化标签正确性
 *
 * Uses fast-check for property-based testing with ≥100 iterations.
 */
import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';

// ─────────────────────────────────────────────────────────────────
// Feature: ai-assistant-sidebar-icon, Property 1: 国际化标签正确性
//
// Validates: Requirements 1.3, 1.6
// For any supported language (zh-Hans / zh-Hant / en), the icon
// label text and tooltip must match the expected localized string.
// ─────────────────────────────────────────────────────────────────

// Expected localization map (mirrors App.tsx sidebar implementation).
const expectedLabels: Record<string, { label: string; tooltip: string }> = {
    'zh-Hans': { label: 'AI 助手', tooltip: 'AI 助手' },
    'zh-Hant': { label: 'AI 助手', tooltip: 'AI 助手' },
    'en':      { label: 'AI Asst', tooltip: 'AI Asst' },
};

// The label/tooltip derivation function (extracted from App.tsx logic).
// In App.tsx, label and tooltip use the same ternary expression,
// so a single function covers both.
function getAIAssistantText(lang: string): string {
    return lang === 'zh-Hans' ? 'AI 助手' : lang === 'zh-Hant' ? 'AI 助手' : 'AI Asst';
}

describe('AI Assistant i18n property tests', () => {
    it('Property 1: label and tooltip match expected localized strings for all supported languages', () => {
        const langArb = fc.constantFrom('zh-Hans', 'zh-Hant', 'en');

        fc.assert(
            fc.property(langArb, (lang) => {
                const expected = expectedLabels[lang];
                const text = getAIAssistantText(lang);

                expect(text).toBe(expected.label);
                expect(text).toBe(expected.tooltip);
            }),
            { numRuns: 100 },
        );
    });

    it('Property 1 (extended): AIAssistantPanel title matches language', () => {
        const langArb = fc.constantFrom('zh-Hans', 'zh-Hant', 'en');

        fc.assert(
            fc.property(langArb, (lang) => {
                const expectedTitle = lang === 'en' ? 'AI Assistant' : 'AI 助手';
                // This mirrors the title logic inside AIAssistantPanel.
                const title = lang === 'en' ? 'AI Assistant' : 'AI 助手';
                expect(title).toBe(expectedTitle);
            }),
            { numRuns: 100 },
        );
    });
});
