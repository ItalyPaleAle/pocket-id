<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { LucideMail, LucideUser, LucideUsers } from '@lucide/svelte';
	import ScopeItem from './scope-item.svelte';

	let { scope }: { scope: string } = $props();
</script>

<div class="flex flex-col gap-3" data-testid="scopes">
	{#if scope!.includes('email')}
		<ScopeItem icon={LucideMail} name={m.email()} description={m.view_your_email_address()} />
	{/if}
	{#if scope!.includes('profile')}
		<ScopeItem
			icon={LucideUser}
			name={m.profile()}
			description={m.view_your_profile_information()}
		/>
	{/if}
	{#if scope!.includes('groups')}
		<ScopeItem
			icon={LucideUsers}
			name={m.groups()}
			description={m.view_the_groups_you_are_a_member_of()}
		/>
	{/if}
</div>
