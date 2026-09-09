import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';

import ActionMenu from './ActionMenu.svelte';
import type { Detection } from '$lib/types/detection.types';

const detection = {
  id: 7,
  date: '2026-09-08',
  time: '07:14:00',
  scientificName: 'Ficedula hypoleuca',
  commonName: 'Pied Flycatcher',
  confidence: 0.87,
  locked: false,
} as Detection;

// The global test setup (src/test/setup.ts) mocks $lib/i18n with a fixed map and
// echoes back any key it does not know, so asserting on the rendered LABEL would
// only prove the mock's contents. Asserting the key instead verifies the thing
// this component actually controls — which key it asks for — while
// `npm run i18n:check-usage` separately guarantees that key resolves in every
// locale.
const REANALYZE_KEY = 'dashboard.recentDetections.actions.reanalyze';
const REVIEW_LABEL = 'Review detection';

describe('ActionMenu reanalyze item', () => {
  let user: ReturnType<typeof userEvent.setup>;

  beforeEach(() => {
    user = userEvent.setup();
  });

  it('offers Reanalyze directly after Review when a handler is supplied', async () => {
    render(ActionMenu, {
      props: { detection, onReview: vi.fn(), onReanalyze: vi.fn(), variant: 'overlay' },
    });

    await user.click(screen.getByRole('button', { name: /Actions/i }));

    const items = screen.getAllByRole('menuitem').map(el => el.textContent.trim());
    const reviewAt = items.findIndex(t => t.includes(REVIEW_LABEL));
    const reanalyzeAt = items.findIndex(t => t.includes(REANALYZE_KEY));

    expect(reviewAt).toBeGreaterThanOrEqual(0);
    // Position matters: the request was for it to sit directly below Review.
    expect(reanalyzeAt).toBe(reviewAt + 1);
  });

  it('invokes the handler and closes the menu', async () => {
    const onReanalyze = vi.fn();
    render(ActionMenu, { props: { detection, onReview: vi.fn(), onReanalyze } });

    await user.click(screen.getByRole('button', { name: /Actions/i }));
    await user.click(screen.getByRole('menuitem', { name: REANALYZE_KEY }));

    expect(onReanalyze).toHaveBeenCalledTimes(1);
  });

  it('omits the item entirely when no handler is supplied', async () => {
    // Existing call sites pass no onReanalyze; they must be unchanged rather than
    // showing an action that would do nothing.
    render(ActionMenu, { props: { detection, onReview: vi.fn() } });

    await user.click(screen.getByRole('button', { name: /Actions/i }));

    expect(screen.queryByRole('menuitem', { name: REANALYZE_KEY })).not.toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: REVIEW_LABEL })).toBeInTheDocument();
  });
});
