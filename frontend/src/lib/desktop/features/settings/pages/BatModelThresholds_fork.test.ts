import { afterEach, describe, it, expect } from 'vitest';
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte';
import { get } from 'svelte/store';
import { settingsActions, settingsStore } from '$lib/stores/settings';
import BatModelThresholds from './BatModelThresholds_fork.svelte';

afterEach(cleanup);

describe('fork bat thresholds', () => {
  it('inherits the shared threshold, enables an override and preserves model paths', async () => {
    settingsActions.updateSection('bat', { threshold: 0.7 });
    settingsActions.updateSection('forkModels', {
      batDetect2: { threshold: 0, modelPath: '/model.onnx' },
      bsgBat: { threshold: 0.9 },
    });
    render(BatModelThresholds);
    const checkbox = screen.getByRole('checkbox', { name: /BatDetect2/ });
    const input = screen.getByRole('spinbutton', { name: /BatDetect2/ });
    expect(input).toBeDisabled();
    expect(input).toHaveValue(0.7);
    await fireEvent.click(checkbox);
    expect(input).toBeEnabled();
    expect(get(settingsStore).formData.forkModels?.batDetect2).toEqual({
      threshold: 0.7,
      modelPath: '/model.onnx',
    });
    expect(get(settingsStore).formData.forkModels?.bsgBat?.threshold).toBe(0.9);
    await fireEvent.input(input, { target: { value: '0.4' } });
    await fireEvent.change(input);
    expect(get(settingsStore).formData.forkModels?.batDetect2?.threshold).toBe(0.4);
    await fireEvent.click(checkbox);
    expect(get(settingsStore).formData.forkModels?.batDetect2?.threshold).toBe(0);
  });
  it('can enable an override when the shared threshold is zero', async () => {
    settingsActions.updateSection('bat', { threshold: 0 });
    settingsActions.updateSection('forkModels', { batDetect2: { threshold: 0 } });
    render(BatModelThresholds);
    await fireEvent.click(screen.getByRole('checkbox', { name: /BatDetect2/ }));
    expect(get(settingsStore).formData.forkModels?.batDetect2?.threshold).toBeGreaterThan(0);
  });
});
