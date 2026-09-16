<!-- Show/hide and reorder workspace columns. Changes apply immediately. -->
<script lang="ts">
  import { ArrowDown, ArrowUp } from '@lucide/svelte';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import { t } from '$lib/i18n';
  import { getColumn } from '../columns';
  import type { WorkspaceLayout } from '../types';

  interface Props {
    layout: WorkspaceLayout;
    onChange: (_layout: WorkspaceLayout) => void;
  }

  let { layout, onChange }: Props = $props();

  function setVisible(index: number, visible: boolean) {
    onChange({
      ...layout,
      columns: layout.columns.map((c, i) => (i === index ? { ...c, visible } : c)),
    });
  }

  function move(index: number, delta: number) {
    const target = index + delta;
    if (target < 0 || target >= layout.columns.length) return;
    const columns = [...layout.columns];
    const [moved] = columns.splice(index, 1);
    if (!moved) return;
    columns.splice(target, 0, moved);
    onChange({ ...layout, columns });
  }
</script>

<section
  class="rounded-lg border border-[var(--color-base-300)] p-3"
  aria-label={t('speciesWorkspace.edit.panelTitle')}
>
  <h2 class="mb-2 text-sm font-semibold">{t('speciesWorkspace.edit.panelTitle')}</h2>
  <ol class="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
    {#each layout.columns as column, index (column.id)}
      {@const def = getColumn(column.id)}
      {@const label = def ? t(def.labelKey) : column.id}
      <li
        class="flex items-center justify-between gap-2 rounded-md px-2 py-1 hover:bg-[var(--color-base-200)]"
      >
        <Checkbox
          checked={column.visible}
          disabled={def?.mandatory}
          tooltip={def?.mandatory
            ? t('speciesWorkspace.edit.mandatory', { column: label })
            : undefined}
          onchange={checked => setVisible(index, checked)}
        >
          <span aria-hidden="true">{label}</span>
          <span class="sr-only">
            {def?.mandatory
              ? t('speciesWorkspace.edit.mandatory', { column: label })
              : t('speciesWorkspace.edit.show', { column: label })}
          </span>
        </Checkbox>
        <span class="flex gap-1">
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-square"
            disabled={index === 0}
            aria-label={t('speciesWorkspace.edit.moveUp', { column: label })}
            onclick={() => move(index, -1)}
          >
            <ArrowUp class="size-3.5" />
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-square"
            disabled={index === layout.columns.length - 1}
            aria-label={t('speciesWorkspace.edit.moveDown', { column: label })}
            onclick={() => move(index, 1)}
          >
            <ArrowDown class="size-3.5" />
          </button>
        </span>
      </li>
    {/each}
  </ol>
</section>
