import { t } from '$lib/i18n';
import type { Column, Membership } from './types';
export function columnLabel(column: Column): string {
  switch (column) {
    case 'count':
      return t('analytics.species.headers.detections');
    case 'max_confidence':
      return t('analytics.species.headers.maxConfidence');
    case 'last_heard':
      return t('analytics.species.headers.lastDetected');
    case 'correct':
      return t('analytics.speciesTools.manage.headers.reviewRatio');
    case 'range':
      return t('analytics.speciesTools.manage.headers.rangeProbability');
    case 'confirmed':
      return t('analytics.speciesTools.manage.headers.confirmed');
    case 'included':
      return t('analytics.speciesTools.manage.headers.included');
    case 'excluded':
      return t('analytics.speciesTools.manage.headers.excluded');
    case 'best':
      return t('analytics.speciesTools.bestRecording');
  }
}
export function actionLabel(kind: Membership): string {
  switch (kind) {
    case 'confirmed':
      return t('analytics.speciesTools.toggleConfirmed');
    case 'included':
      return t('analytics.speciesTools.toggleIncluded');
    case 'excluded':
      return t('analytics.speciesTools.toggleExcluded');
  }
}
