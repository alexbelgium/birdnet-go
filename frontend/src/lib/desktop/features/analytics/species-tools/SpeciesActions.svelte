<script lang="ts">
  import { t } from '$lib/i18n';
  import { MoreHorizontal } from '@lucide/svelte';
  import Button from '$lib/desktop/components/ui/Button.svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import { membershipNames, type SpeciesRow } from './types';
  import { actionLabel } from './labels';
  import type { Workspace } from './workspace.svelte';
  let {
    workspace,
    species,
    onDelete,
  }: { workspace: Workspace; species: SpeciesRow; onDelete: (_species: SpeciesRow) => void } =
    $props();
  let open = $state(false);
  function show() {
    open = true;
    for (const kind of membershipNames) void workspace.loadMembers(kind);
  }
</script>

<Button variant="ghost" onclick={show} aria-label={t('detections.headers.actions')}
  ><MoreHorizontal class="size-4" /></Button
>
<Modal isOpen={open} title={t('detections.headers.actions')} onClose={() => (open = false)}>
  <div class="space-y-2">
    {#each membershipNames as kind (kind)}
      <Button
        className="w-full justify-between"
        disabled={workspace.member(kind, species) === null ||
          workspace.pending.has(`toggle-${kind}`)}
        onclick={() => workspace.toggle(kind, species)}
      >
        {actionLabel(kind)}
        <span
          >{workspace.member(kind, species) === null
            ? t('common.ui.loading')
            : workspace.member(kind, species)
              ? t('common.yes')
              : t('common.no')}</span
        >
      </Button>
      {#if workspace.errors.has(kind)}<p role="alert">{workspace.errors.get(kind)}</p>
        <Button onclick={() => workspace.loadMembers(kind)}>{t('common.retry')}</Button>{/if}
    {/each}
    <Button
      variant="error"
      className="w-full"
      onclick={() => {
        open = false;
        onDelete(species);
      }}>{t('analytics.speciesTools.manage.delete')}</Button
    >
  </div>
</Modal>
