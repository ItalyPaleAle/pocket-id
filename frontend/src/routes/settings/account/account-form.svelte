<script lang="ts">
	import FormInput from '$lib/components/form/form-input.svelte';
	import { Button } from '$lib/components/ui/button';
	import type { UserCreate } from '$lib/types/user.type';
	import { createForm } from '$lib/utils/form-util';
	import { z } from 'zod';

	let {
		callback,
		account
	}: {
		account: UserCreate;
		callback: (user: UserCreate) => Promise<boolean>;
	} = $props();

	let isLoading = $state(false);

	const formSchema = z.object({
		firstName: z.string().min(1).max(50),
		lastName: z.string().min(1).max(50),
		username: z
			.string()
			.min(2)
			.max(30)
			.regex(
				/^[a-z0-9_@.-]+$/,
				"Username can only contain lowercase letters, numbers, underscores, dots, hyphens, and '@' symbols"
			),
		email: z.string().email(),
		isAdmin: z.boolean()
	});
	type FormSchema = typeof formSchema;

	const { inputs, ...form } = createForm<FormSchema>(formSchema, account);
	async function onSubmit() {
		const data = form.validate();
		if (!data) return;
		isLoading = true;
		const success = await callback(data);
		// Reset form if user was successfully created
		isLoading = false;
	}
</script>

<form onsubmit={onSubmit}>
	<div class="flex flex-col gap-3 sm:flex-row">
		<div class="w-full">
			<FormInput label="First name" bind:input={$inputs.firstName} />
		</div>
		<div class="w-full">
			<FormInput label="Last name" bind:input={$inputs.lastName} />
		</div>
	</div>
	<div class="mt-3 flex flex-col gap-3 sm:flex-row">
		<div class="w-full">
			<FormInput label="Email" bind:input={$inputs.email} />
		</div>
		<div class="w-full">
			<FormInput label="Username" bind:input={$inputs.username} />
		</div>
	</div>
	<div class="mt-5 flex justify-end">
		<Button {isLoading} type="submit">Save</Button>
	</div>
</form>
