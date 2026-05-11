package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clawreef/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// ConnectionMode defines how to connect to Kubernetes
type ConnectionMode string

const (
	// ModeAuto automatically detects in-cluster or out-of-cluster
	ModeAuto ConnectionMode = "auto"
	// ModeInCluster forces in-cluster configuration
	ModeInCluster ConnectionMode = "incluster"
	// ModeOutOfCluster forces out-of-cluster configuration using kubeconfig
	ModeOutOfCluster ConnectionMode = "outofcluster"
)

// Client wraps the Kubernetes client
type Client struct {
	Clientset      kubernetes.Interface
	Config         *rest.Config
	Namespace      string
	StorageClass   string
	HostPathPrefix string
	Mode           ConnectionMode
}

var (
	// globalClient is the global K8s client instance
	globalClient        *Client
	k8sNameInvalidChars = regexp.MustCompile(`[^a-z0-9-]+`)
	k8sNameExtraDashes  = regexp.MustCompile(`-+`)
)

// Initialize initializes the Kubernetes client with support for both in-cluster and out-of-cluster modes
// Configuration priority:
// 1. configs/k8s.yaml - Main configuration file
// 2. Environment variables - Override config file settings
func Initialize(cfg *config.Config) error {
	mode := ConnectionMode(cfg.GetMode())
	if mode == "" {
		mode = ModeAuto
	}

	var restConfig *rest.Config
	var err error
	var detectedMode ConnectionMode

	switch mode {
	case ModeInCluster:
		restConfig, err = buildInClusterConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize in-cluster config: %w", err)
		}
		detectedMode = ModeInCluster

	case ModeOutOfCluster:
		restConfig, err = buildOutOfClusterConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize out-of-cluster config: %w", err)
		}
		detectedMode = ModeOutOfCluster

	case ModeAuto:
		// Try in-cluster first
		restConfig, err = buildInClusterConfig(cfg)
		if err == nil {
			detectedMode = ModeInCluster
		} else {
			// Fall back to out-of-cluster
			restConfig, err = buildOutOfClusterConfig(cfg)
			if err != nil {
				return fmt.Errorf("failed to initialize K8s client (both in-cluster and out-of-cluster failed): %w", err)
			}
			detectedMode = ModeOutOfCluster
		}

	default:
		return fmt.Errorf("invalid K8S_MODE: %s (must be 'auto', 'incluster', or 'outofcluster')", mode)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	globalClient = &Client{
		Clientset:      clientset,
		Config:         restConfig,
		Namespace:      cfg.GetNamespace(),
		StorageClass:   cfg.GetStorageClass(),
		HostPathPrefix: cfg.GetHostPathPrefix(),
		Mode:           detectedMode,
	}

	return nil
}

// buildInClusterConfig builds REST config for in-cluster mode
func buildInClusterConfig(cfg *config.Config) (*rest.Config, error) {
	// Use standard in-cluster configuration
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// Apply timeout from config
	if cfg.Kubernetes.Common.Timeout > 0 {
		restConfig.Timeout = time.Duration(cfg.Kubernetes.Common.Timeout) * time.Second
	}

	return restConfig, nil
}

// buildOutOfClusterConfig builds REST config from kubeconfig file
func buildOutOfClusterConfig(cfg *config.Config) (*rest.Config, error) {
	kubeconfig := cfg.GetKubeconfigPath()

	// Priority for kubeconfig location:
	// 1. Config file setting (configs/k8s.yaml -> kubernetes.outOfCluster.kubeconfig)
	// 2. KUBECONFIG environment variable
	// 3. K8S_KUBECONFIG environment variable
	// 4. Default location (~/.kube/config)
	if kubeconfig == "" {
		if kubeconfig = os.Getenv("KUBECONFIG"); kubeconfig == "" {
			if kubeconfig = os.Getenv("K8S_KUBECONFIG"); kubeconfig == "" {
				home := homedir.HomeDir()
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}
	}

	// Check if kubeconfig file exists
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig file not found at %s", kubeconfig)
	}

	// Build config from kubeconfig file
	loadingRules := &clientcmd.ClientConfigLoadingRules{
		ExplicitPath: kubeconfig,
	}

	configOverrides := &clientcmd.ConfigOverrides{}

	// Override context if specified
	if cfg.Kubernetes.OutOfCluster.Context != "" {
		configOverrides.CurrentContext = cfg.Kubernetes.OutOfCluster.Context
	}

	// Override API server if specified
	if cfg.Kubernetes.OutOfCluster.APIServer != "" {
		configOverrides.ClusterInfo.Server = cfg.Kubernetes.OutOfCluster.APIServer
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig %s: %w", kubeconfig, err)
	}

	// Apply TLS settings
	if cfg.Kubernetes.OutOfCluster.TLS.InsecureSkipVerify {
		restConfig.Insecure = true
		restConfig.TLSClientConfig.Insecure = true
	}

	if cfg.Kubernetes.OutOfCluster.TLS.CAFile != "" {
		restConfig.TLSClientConfig.CAFile = cfg.Kubernetes.OutOfCluster.TLS.CAFile
	}

	if cfg.Kubernetes.OutOfCluster.TLS.CertFile != "" {
		restConfig.TLSClientConfig.CertFile = cfg.Kubernetes.OutOfCluster.TLS.CertFile
	}

	if cfg.Kubernetes.OutOfCluster.TLS.KeyFile != "" {
		restConfig.TLSClientConfig.KeyFile = cfg.Kubernetes.OutOfCluster.TLS.KeyFile
	}

	// Apply timeout from config
	if cfg.Kubernetes.Common.Timeout > 0 {
		restConfig.Timeout = time.Duration(cfg.Kubernetes.Common.Timeout) * time.Second
	}

	return restConfig, nil
}

// GetClient returns the global Kubernetes client
func GetClient() *Client {
	return globalClient
}

// IsInCluster returns true if running in-cluster
func (c *Client) IsInCluster() bool {
	return c.Mode == ModeInCluster
}

// IsOutOfCluster returns true if running out-of-cluster
func (c *Client) IsOutOfCluster() bool {
	return c.Mode == ModeOutOfCluster
}

// GetConnectionMode returns the current connection mode
func (c *Client) GetConnectionMode() ConnectionMode {
	return c.Mode
}

// GetNamespace returns the namespace for an instance
func (c *Client) GetNamespace(userID int) string {
	return sanitizeK8sName(fmt.Sprintf("%s-user-%d", c.Namespace, userID))
}

// GetSystemNamespace returns the control-plane namespace used by ClawManager itself.
func (c *Client) GetSystemNamespace() string {
	return sanitizeK8sName(fmt.Sprintf("%s-system", c.Namespace))
}

// GetPodName returns the pod name for an instance
func (c *Client) GetPodName(instanceID int, instanceName string) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-%s", instanceID, instanceName))
}

// GetPVCName returns the PVC name for an instance
func (c *Client) GetPVCName(instanceID int) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-pvc", instanceID))
}

// GetServiceName returns the service name for an instance
func (c *Client) GetServiceName(instanceID int, instanceName string) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-%s-svc", instanceID, instanceName))
}

// GetOpenClawBootstrapSecretName returns the secret name used to store rendered OpenClaw bootstrap payloads.
func (c *Client) GetOpenClawBootstrapSecretName(instanceID int, instanceName string) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-%s-openclaw-bootstrap", instanceID, instanceName))
}

// GetNetworkPolicyName returns the default network policy name for an instance.
func (c *Client) GetNetworkPolicyName(instanceID int, instanceName string) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-%s-netpol", instanceID, instanceName))
}

func sanitizeK8sName(name string) string {
	sanitized := strings.ToLower(name)
	sanitized = k8sNameInvalidChars.ReplaceAllString(sanitized, "-")
	sanitized = k8sNameExtraDashes.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")

	if sanitized == "" {
		return "clawreef"
	}

	if len(sanitized) > 63 {
		sanitized = strings.Trim(sanitized[:63], "-")
		if sanitized == "" {
			return "clawreef"
		}
	}

	return sanitized
}
