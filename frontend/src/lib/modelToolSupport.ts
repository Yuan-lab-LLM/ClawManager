interface ToolCapability {
  tools_support_override?: boolean | null;
  supports_tools?: boolean;
  capability_source?: string;
  classification_source?: string;
}

// null means unknown, not an assertion that tools are unsupported.
export function modelToolSupport(model: ToolCapability): boolean | null {
  if (model.tools_support_override != null) return model.tools_support_override;
  if (model.supports_tools) return true;
  if ((model.capability_source || model.classification_source) === 'provider_metadata') return false;
  return null;
}
