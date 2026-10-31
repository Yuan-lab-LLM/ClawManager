package k8s

// NewTeamProvisioningServices shares one explicit Kubernetes client. This keeps
// provisioning dependencies coherent without replacing the process-wide client.
func NewTeamProvisioningServices(client *Client) (*PVCService, *SecretService, *ConfigMapService) {
	ns := &NamespaceService{client: client}
	return &PVCService{client: client, namespaceService: ns}, &SecretService{client: client, namespaceService: ns}, &ConfigMapService{client: client, namespaceService: ns}
}
