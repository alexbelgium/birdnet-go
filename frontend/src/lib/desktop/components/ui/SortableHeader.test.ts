import { describe, it, expect, afterEach, vi } from 'vitest';
import { cleanup, fireEvent } from '@testing-library/svelte';
import { createComponentTestFactory } from '../../../../test/render-helpers';
import SortableHeader from './SortableHeader.svelte';

const headerTest = createComponentTestFactory(SortableHeader);

describe('SortableHeader', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders the label and a field-specific testid', () => {
    const { getByTestId } = headerTest.render({
      label: 'Detections',
      field: 'count',
      activeField: 'species',
      direction: 'asc',
      onSort: vi.fn(),
    });

    const button = getByTestId('sort-count');
    expect(button).toBeInTheDocument();
    expect(button).toHaveTextContent('Detections');
  });

  it('marks the header as ascending when active and ascending', () => {
    const { container } = headerTest.render({
      label: 'Species',
      field: 'species',
      activeField: 'species',
      direction: 'asc',
      onSort: vi.fn(),
    });

    const th = container.querySelector('th');
    expect(th).toHaveAttribute('aria-sort', 'ascending');
    // ChevronUp is rendered as an <svg> inside the indicator span.
    expect(container.querySelector('svg')).toBeInTheDocument();
  });

  it('marks the header as descending when active and descending', () => {
    const { container } = headerTest.render({
      label: 'Detections',
      field: 'count',
      activeField: 'count',
      direction: 'desc',
      onSort: vi.fn(),
    });

    expect(container.querySelector('th')).toHaveAttribute('aria-sort', 'descending');
  });

  it('reports no active sort when the column is inactive', () => {
    const { container } = headerTest.render({
      label: 'Detections',
      field: 'count',
      activeField: 'species',
      direction: 'asc',
      onSort: vi.fn(),
    });

    expect(container.querySelector('th')).toHaveAttribute('aria-sort', 'none');
  });

  it('calls onSort with the field when clicked', async () => {
    const onSort = vi.fn();
    const { getByTestId } = headerTest.render({
      label: 'First Detected',
      field: 'first_seen',
      activeField: 'count',
      direction: 'desc',
      onSort,
    });

    await fireEvent.click(getByTestId('sort-first_seen'));
    expect(onSort).toHaveBeenCalledWith('first_seen');
  });
});
