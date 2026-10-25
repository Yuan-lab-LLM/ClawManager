import type { ComponentProps } from 'react'

// No native preview pane or URL title fetch: only explicit HTTPS/HTTP user clicks.
export function ExternalLink({ href, children, ...props }: ComponentProps<'a'>) {
  if (!href || !/^https?:\/\//i.test(href)) return <span>{children}</span>
  return <a {...props} href={href} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">{children}</a>
}
