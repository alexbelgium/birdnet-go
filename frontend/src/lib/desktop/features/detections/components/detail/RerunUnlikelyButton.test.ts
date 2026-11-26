import { cleanup, fireEvent, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '$lib/utils/api';
import { renderTyped } from '../../../../../../test/render-helpers';
import ReviewPanel from './ReviewPanel.svelte';
import type { DetectionDetailData } from './detailTypes';

const buttonLabel = 'detections.detail.review.rerunUnlikely';
const bird: DetectionDetailData = {
  id: 7,
  date: '2026-11-14',
  time: '02:57:03',
  beginTime: '2026-11-14T02:57:00+02:00',
  endTime: '2026-11-14T02:57:03+02:00',
  speciesCode: 'eurwoo',
  scientificName: 'Scolopax rusticola',
  commonName: 'Eurasian Woodcock',
  confidence: 0.87,
  verified: 'unverified',
  locked: false,
  modelType: 'bird',
};
const bat: DetectionDetailData = {
  ...bird,
  scientificName: 'Pipistrellus pipistrellus',
  commonName: 'Common pipistrelle',
  modelType: 'bat',
  canRerunUnlikely: true,
};

function renderReview(detection: DetectionDetailData) {
  const onChanged = vi.fn();
  renderTyped(ReviewPanel, {
    props: {
      detection,
      speciesName: detection.commonName,
      canReview: true,
      speciesBlockedReason: '',
      onWrongSpecies: vi.fn(),
      onChanged,
    },
  });
  return onChanged;
}

describe('RerunUnlikelyButton', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it.each([bird, { ...bat, canRerunUnlikely: false }])(
    'hides the action for an ineligible $modelType detection',
    detection => {
      renderReview(detection);
      expect(screen.queryByRole('button', { name: buttonLabel })).not.toBeInTheDocument();
    }
  );

  it('re-runs the filter for an eligible bat and reloads that detection', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      outcome: 'probable',
      unlikely: false,
      ultrasonicCV: 0.3,
      ultrasonicCVThreshold: 0.15,
    });
    const onChanged = renderReview(bat);
    await fireEvent.click(screen.getByRole('button', { name: buttonLabel }));

    expect(post).toHaveBeenCalledExactlyOnceWith('/api/v2/detections/7/rerun-unlikely');
    await waitFor(() =>
      expect(onChanged).toHaveBeenCalledExactlyOnceWith({
        detectionId: bat.id,
        scientificName: bat.scientificName,
        speciesStats: false,
      })
    );
  });

  it('reloads even when the verdict changed nothing', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      outcome: 'unchanged',
      unlikely: true,
      ultrasonicCV: 0.05,
      ultrasonicCVThreshold: 0.15,
    });
    const onChanged = renderReview(bat);
    await fireEvent.click(screen.getByRole('button', { name: buttonLabel }));
    await waitFor(() => expect(post).toHaveBeenCalledOnce());
    await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
  });

  it('blocks a kept detection from being changed', async () => {
    const post = vi.spyOn(api, 'post');
    renderReview({ ...bat, locked: true });
    const button = screen.getByRole('button', { name: buttonLabel });
    expect(button).toHaveAttribute('aria-disabled', 'true');
    await fireEvent.click(button);
    expect(post).not.toHaveBeenCalled();
  });
});
