// Runtime filesystem media is intentionally not fetched by this core renderer.
export function mediaDisplayLabel(value: string): string {
  return value.split(/[\\/]/).pop() || '附件'
}
export function mediaMarkdownHref(value: string): string {
  return /^https?:\/\//i.test(value) ? value : '#unsupported-workspace-media'
}
