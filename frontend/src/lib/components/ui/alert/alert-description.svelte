<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils/style.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="alert-description"
	class={cn('col-start-2 grid justify-items-start gap-1 text-sm [&_p]:leading-relaxed', className)}
	{...restProps}
>
	{@render children?.()}
</div>
