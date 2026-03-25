import { useI18n } from '../i18n'

export default function LanguageToggle({ className = '' }) {
    const { lang, setLang, t } = useI18n()
    const nextLang = lang === 'zh' ? 'en' : 'zh'
    const label = nextLang === 'zh' ? t('language.chinese') : t('language.english')

    return (
        <button
            type="button"
            onClick={() => setLang(nextLang)}
            className={`text-xs font-semibold px-2 py-1 rounded-md border border-border bg-secondary/50 text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors ${className}`}
            title={t('language.label')}
        >
            {label}
        </button>
    )
}
