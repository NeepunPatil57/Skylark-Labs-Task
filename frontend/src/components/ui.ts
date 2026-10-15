// Shared class strings for controls used in more than one component.

export const control =
  'h-10 w-full rounded-md border border-line bg-field px-3 placeholder:text-[#5c6b7c] aria-invalid:border-error'

const button = 'cursor-pointer whitespace-nowrap rounded-md border border-transparent px-[18px] font-semibold'

export const primaryButton = `${button} h-10 bg-accent text-accent-ink enabled:hover:bg-accent-hover disabled:cursor-progress disabled:opacity-60`

export const ghostButton = `${button} h-[34px] border-muted bg-white/5 text-ink hover:border-ink hover:bg-white/10`

/** Small outlined button for the toolbar (test cameras, column picker). */
export const chip =
  'h-[30px] cursor-pointer rounded-md border border-line bg-transparent px-[11px] enabled:hover:border-muted'
