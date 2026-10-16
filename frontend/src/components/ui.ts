// Shared class strings for controls used in more than one component.

export const control =
  'h-10 w-full rounded-md border border-line bg-field px-3 placeholder:text-faint aria-invalid:border-error'

const button = 'cursor-pointer whitespace-nowrap rounded-md border border-transparent px-4.5 font-semibold'

export const primaryButton = `${button} h-10 bg-brand text-white shadow-[0_0_24px_rgb(91_33_182/0.35)] enabled:hover:bg-brand-hover disabled:cursor-progress disabled:opacity-60`

export const ghostButton = `${button} h-8.5 border-muted bg-white/5 text-ink hover:border-ink hover:bg-white/10`

/** Small spaced-out monospace caption, used for field and toolbar labels. */
export const caption = 'font-mono text-[11px] font-medium uppercase tracking-[0.18em] text-muted'

/** Small outlined button for the toolbar (test cameras, column picker). */
export const chip =
  'h-7.5 cursor-pointer rounded-md border border-line bg-transparent px-2.75 enabled:hover:border-muted'
