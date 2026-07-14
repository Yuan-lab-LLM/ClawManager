import type { Locale, TranslationTree } from '../i18n';
import { protectionTranslations } from './protectionI18n';
import { inputDetectionTranslations } from './inputDetectionI18n';
import { secureClawTranslations } from './secureClawI18n';
import { eventsTranslations } from './eventsI18n';
import { runtimeTranslations } from './runtimeI18n';
import { hostHardeningTranslations } from './hostHardeningI18n';
import { outboundTranslations } from './outboundI18n';
import { policyTranslations } from './policyI18n';
import { governTranslations } from './governI18n';

/** Deep-merge multiple TranslationTree objects (shallow per level). */
function mergeTrees(...trees: TranslationTree[]): TranslationTree {
  const out: TranslationTree = {};
  for (const tree of trees) {
    for (const key of Object.keys(tree)) {
      const val = tree[key];
      if (typeof val === 'object' && val !== null && !Array.isArray(val)) {
        out[key] = mergeTrees(out[key] as TranslationTree ?? {}, val);
      } else {
        out[key] = val;
      }
    }
  }
  return out;
}

function mergeLocaleTranslations(getters: Array<(locale: Locale) => TranslationTree>): Record<Locale, TranslationTree> {
  return {
    en: mergeTrees(...getters.map((g) => g('en'))),
    zh: mergeTrees(...getters.map((g) => g('zh'))),
    ja: mergeTrees(...getters.map((g) => g('ja'))),
    ko: mergeTrees(...getters.map((g) => g('ko'))),
    de: mergeTrees(...getters.map((g) => g('de'))),
  };
}

export const secplaneTranslations: Record<Locale, TranslationTree> = mergeLocaleTranslations([
  (l) => ({ protection: protectionTranslations[l] }),
  (l) => ({ inputDetection: inputDetectionTranslations[l] }),
  (l) => ({ secureClaw: secureClawTranslations[l] }),
  (l) => ({ events: eventsTranslations[l] }),
  (l) => ({ runtime: runtimeTranslations[l] }),
  (l) => ({ protection: { hostHardening: hostHardeningTranslations[l] } }),
  (l) => ({ protection: { outbound: outboundTranslations[l] } }),
  (l) => ({ protection: { policy: policyTranslations[l] } }),
  (l) => ({ protection: { govern: governTranslations[l] } }),
]);
