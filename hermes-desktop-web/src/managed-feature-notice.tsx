export function ManagedFeatureNotice({ feature }: { feature: string }) {
  return <section role="note" className="flex h-full min-h-24 flex-col items-center justify-center gap-2 p-6 text-center text-sm text-muted-foreground">
    <strong>{feature}</strong>
    <p>此能力未在 ClawManager Web 中开放。不会访问您的本机，也不会将请求透传到实例主机。</p>
  </section>
}
