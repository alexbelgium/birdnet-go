<!-- Per-species actions: toggle confirmed / include / exclude, delete. -->
<script lang="ts">
  import { EllipsisVertical, Trash2 } from '@lucide/svelte';
  import { t } from '$lib/i18n';
  import { toastActions } from '$lib/stores/toast';
  import type { MembershipKind } from '../types';
  import type { WorkspaceData } from '../workspaceData.svelte';
  import type { SpeciesRow } from './rows';

  interface Props {
    row: SpeciesRow;
    data: WorkspaceData;
    onDelete: (_row: SpeciesRow) => void;
  }

  let { row, data, onDelete }: Props = $props();

  let open = $state(false);
  let container: HTMLDivElement | undefined = $state();
  const menuId = `species-actions-${Math.random().toString(36).slice(2, 10)}`;

  const KINDS: MembershipKind[] = ['confirmed', 'included', 'excluded'];
  const loaded = $derived(data.status.memberships === 'ready');

  async function toggle(kind: MembershipKind) {
    const member = data.isMember(kind, row.scientificName);
    if (member === null) return;
    open = false;
    try {
      await data.setMembership(kind, row.scientificName, !member);
    } catch {
      toastActions.error(
        t('speciesWorkspace.actions.updateFailed', {
          list: t(`speciesWorkspace.membership.${kind}`),
          species: row.displayName,
        })
      );
    }
  }

  $effect(() => {
    if (!open) return;
    const close = (event: Event) => {
      if (
        event instanceof KeyboardEvent
          ? event.key === 'Escape'
          : !container?.contains(event.target as Node)
      ) {
        open = false;
      }
    };
    document.addEventListener('click', close);
    document.addEventListener('keydown', close);
    return () => {
      document.removeEventListener('click', close);
      document.removeEventListener('keydown', close);
    };
  });
</script>

<div class="relative inline-block" bind:this={container}>
  <button
    type="button"
    class="btn btn-ghost btn-xs btn-square"
    aria-haspopup="menu"
    aria-expanded={open}
    aria-controls={menuId}
    aria-label={t('speciesWorkspace.actions.menu', { species: row.displayName })}
    onclick={() => (open = !open)}
  >
    <EllipsisVertical class="size-4" />
  </button>
  {#if open}
    <ul
      id={menuId}
      role="menu"
      class="absolute right-0 z-[1100] mt-1 w-56 rounded-lg border border-[var(--color-base-300)] bg-[var(--color-base-100)] p-1 shadow-lg"
    >
      {#each KINDS as kind (kind)}
        {@const member = data.isMember(kind, row.scientificName)}
        {@const pending = data.togglePending.has(`${kind}:${row.scientificName}`)}
        <li role="none">
          <button
            type="button"
            role="menuitemcheckbox"
            aria-checked={member === true}
            aria-disabled={!loaded || pending}
            title={!loaded ? t('speciesWorkspace.states.membershipLoading') : undefined}
            class="flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm hover:bg-[var(--color-base-200)] aria-disabled:opacity-50"
            onclick={() => loaded && !pending && toggle(kind)}
          >
            <span>
              {t(`speciesWorkspace.actions.toggle.${kind}`, { species: row.displayName })}
            </span>
            {#if member}<span class="badge badge-primary badge-xs"
                >{t('speciesWorkspace.membership.yes')}</span
              >{/if}
          </button>
        </li>
      {/each}
      {#if !loaded}
        <li role="none" class="px-3 py-1 text-xs opacity-60">
          {data.status.memberships === 'error'
            ? t('speciesWorkspace.states.loadFailed', {
                what: t('speciesWorkspace.groups.memberships'),
              })
            : t('speciesWorkspace.states.membershipLoading')}
        </li>
      {/if}
      <li role="none" class="my-1 border-t border-[var(--color-base-300)]"></li>
      <li role="none">
        <button
          type="button"
          role="menuitem"
          class="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-[var(--color-error)] hover:bg-[var(--color-base-200)]"
          onclick={() => {
            open = false;
            onDelete(row);
          }}
        >
          <Trash2 class="size-4" />
          {t('speciesWorkspace.actions.delete', { species: row.displayName })}
        </button>
      </li>
    </ul>
  {/if}
</div>
