import en from '../i18n/en.json';

// Translations by locale. English is the default; add a JSON file in webapp/i18n and register it
// here to add a language.
const translations: Record<string, Record<string, string>> = {
    en,
};

export function getTranslations(locale: string): Record<string, string> {
    return translations[locale] || translations[locale.split('-')[0]] || translations.en;
}

// translate formats a message outside React (for example for desktop notifications). It supports
// simple "{name}" placeholders.
export function translate(locale: string, id: string, defaultMessage: string, values: Record<string, string | number> = {}): string {
    const template = getTranslations(locale)[id] || translations.en[id] || defaultMessage;
    return template.replace(/\{(\w+)\}/g, (match, key) => (key in values ? String(values[key]) : match));
}
