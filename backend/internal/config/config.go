package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	JWT           JWTConfig           `yaml:"jwt"`
	Kubernetes    KubernetesConfig    `yaml:"kubernetes"`
	ObjectStorage ObjectStorageConfig `yaml:"objectStorage"`
	SkillScanner  SkillScannerConfig  `yaml:"skillScanner"`
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Address string `yaml:"address"`
	Mode    string `yaml:"mode"`
}

// DatabaseConfig holds database-related configuration
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

// JWTConfig holds JWT-related configuration
type JWTConfig struct {
	Secret        string `yaml:"secret"`
	AccessExpiry  int    `yaml:"access_expiry"`  // minutes
	RefreshExpiry int    `yaml:"refresh_expiry"` // hours
}

// KubernetesConfig holds Kubernetes-related configuration
type KubernetesConfig struct {
	Mode         string                 `yaml:"mode"` // 连接模式: auto, incluster, outofcluster
	OutOfCluster OutOfClusterConfig     `yaml:"outOfCluster"`
	InCluster    InClusterConfig        `yaml:"inCluster"`
	Common       CommonKubernetesConfig `yaml:"common"`
	Runtime      RuntimeConfig          `yaml:"runtime"`
	Logging      LoggingConfig          `yaml:"logging"`
}

// OutOfClusterConfig holds out-of-cluster Kubernetes configuration
type OutOfClusterConfig struct {
	Kubeconfig string    `yaml:"kubeconfig"`
	Context    string    `yaml:"context"`
	APIServer  string    `yaml:"apiServer"`
	TLS        TLSConfig `yaml:"tls"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
	CAFile             string `yaml:"caFile"`
	CertFile           string `yaml:"certFile"`
	KeyFile            string `yaml:"keyFile"`
}

// InClusterConfig holds in-cluster Kubernetes configuration
type InClusterConfig struct {
	TokenPath     string `yaml:"tokenPath"`
	CAPath        string `yaml:"caPath"`
	NamespacePath string `yaml:"namespacePath"`
}

// CommonKubernetesConfig holds common Kubernetes configuration
type CommonKubernetesConfig struct {
	Namespace           string `yaml:"namespace"`
	StorageClass        string `yaml:"storageClass"`
	Timeout             int    `yaml:"timeout"`
	RetryCount          int    `yaml:"retryCount"`
	AutoCreateNamespace bool   `yaml:"autoCreateNamespace"`
}

// RuntimeConfig holds runtime configuration
type RuntimeConfig struct {
	Pod RuntimePodConfig `yaml:"pod"`
	PVC RuntimePVCConfig `yaml:"pvc"`
}

// RuntimePodConfig holds pod runtime configuration
type RuntimePodConfig struct {
	ImageRegistry string            `yaml:"imageRegistry"`
	ContainerPort int32             `yaml:"containerPort"`
	MountPath     string            `yaml:"mountPath"`
	Privileged    bool              `yaml:"privileged"`
	ExtraLabels   map[string]string `yaml:"extraLabels"`
	NodeSelector  map[string]string `yaml:"nodeSelector"`
	Tolerations   []Toleration      `yaml:"tolerations"`
}

// Toleration represents a Kubernetes toleration
type Toleration struct {
	Key      string `yaml:"key"`
	Operator string `yaml:"operator"`
	Value    string `yaml:"value"`
	Effect   string `yaml:"effect"`
}

// RuntimePVCConfig holds PVC runtime configuration
type RuntimePVCConfig struct {
	AccessMode           string `yaml:"accessMode"`
	VolumeMode           string `yaml:"volumeMode"`
	AllowVolumeExpansion bool   `yaml:"allowVolumeExpansion"`
	ReclaimPolicy        string `yaml:"reclaimPolicy"`
	HostPathPrefix       string `yaml:"hostPathPrefix"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level       string `yaml:"level"`
	LogAPICalls bool   `yaml:"logApiCalls"`
}

type ObjectStorageConfig struct {
	Endpoint       string `yaml:"endpoint"`
	Region         string `yaml:"region"`
	AccessKey      string `yaml:"accessKey"`
	SecretKey      string `yaml:"secretKey"`
	Bucket         string `yaml:"bucket"`
	UseSSL         bool   `yaml:"useSSL"`
	BasePath       string `yaml:"basePath"`
	ForcePathStyle bool   `yaml:"forcePathStyle"`
	LocalFallback  string `yaml:"localFallback"`
}

type SkillScannerConfig struct {
	BaseURL   string `yaml:"baseUrl"`
	APIKey    string `yaml:"apiKey"`
	TimeoutSeconds int `yaml:"timeoutSeconds"`
	Enabled   bool   `yaml:"enabled"`
}

// Load loads configuration from file and environment variables
func Load() (*Config, error) {
	config := &Config{
		Server: ServerConfig{
			Address: ":9001",
			Mode:    "debug",
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     3306,
			User:     "clawreef",
			Password: "clawreef123",
			Database: "clawreef",
		},
		JWT: JWTConfig{
			Secret:        getEnv("JWT_SECRET", "clawreef-secret-key-change-in-production"),
			AccessExpiry:  60,  // 60 minutes
			RefreshExpiry: 168, // 7 days
		},
		Kubernetes: KubernetesConfig{
			Mode: getEnv("K8S_MODE", "auto"),
			OutOfCluster: OutOfClusterConfig{
				Kubeconfig: getEnv("KUBECONFIG", getEnv("K8S_KUBECONFIG", "")),
			},
			InCluster: InClusterConfig{
				TokenPath:     "/var/run/secrets/kubernetes.io/serviceaccount/token",
				CAPath:        "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
				NamespacePath: "/var/run/secrets/kubernetes.io/serviceaccount/namespace",
			},
			Common: CommonKubernetesConfig{
				Namespace:           getEnv("K8S_NAMESPACE", "clawreef"),
				StorageClass:        getEnv("K8S_STORAGE_CLASS", "standard"),
				Timeout:             30,
				RetryCount:          3,
				AutoCreateNamespace: true,
			},
			Runtime: RuntimeConfig{
				Pod: RuntimePodConfig{
					ImageRegistry: "docker.io/clawreef",
					ContainerPort: 3001,
					MountPath:     "/home/user/data",
					Privileged:    false,
					ExtraLabels:   make(map[string]string),
					NodeSelector:  make(map[string]string),
				},
				PVC: RuntimePVCConfig{
					AccessMode:           "ReadWriteOnce",
					VolumeMode:           "Filesystem",
					AllowVolumeExpansion: true,
					ReclaimPolicy:        "Delete",
					HostPathPrefix:       getEnv("K8S_PV_HOST_PATH_PREFIX", "/data/clawreef"),
				},
			},
			Logging: LoggingConfig{
				Level:       "info",
				LogAPICalls: false,
			},
		},
		ObjectStorage: ObjectStorageConfig{
			Endpoint:       getEnv("OBJECT_STORAGE_ENDPOINT", ""),
			Region:         getEnv("OBJECT_STORAGE_REGION", ""),
			AccessKey:      getEnv("OBJECT_STORAGE_ACCESS_KEY", ""),
			SecretKey:      getEnv("OBJECT_STORAGE_SECRET_KEY", ""),
			Bucket:         getEnv("OBJECT_STORAGE_BUCKET", "clawmanager-skills"),
			UseSSL:         strings.EqualFold(getEnv("OBJECT_STORAGE_USE_SSL", "false"), "true"),
			BasePath:       getEnv("OBJECT_STORAGE_BASE_PATH", "skills"),
			ForcePathStyle: strings.EqualFold(getEnv("OBJECT_STORAGE_FORCE_PATH_STYLE", "true"), "true"),
			LocalFallback:  getEnv("OBJECT_STORAGE_LOCAL_FALLBACK", ".data/object-storage"),
		},
		SkillScanner: SkillScannerConfig{
			BaseURL: getEnv("SKILL_SCANNER_BASE_URL", ""),
			APIKey: getEnv("SKILL_SCANNER_API_KEY", ""),
			TimeoutSeconds: 30,
			Enabled: strings.EqualFold(getEnv("SKILL_SCANNER_ENABLED", "false"), "true"),
		},
	}

	// Try to load from k8s config file
	configPaths := []string{
		"configs/k8s.yaml",
		"../configs/k8s.yaml",
		"/app/configs/k8s.yaml",
	}

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Loading k8s config from: %s\n", path)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read k8s config file: %w", err)
			}
			if err := yaml.Unmarshal(data, config); err != nil {
				return nil, fmt.Errorf("failed to parse k8s config file: %w", err)
			}
			break
		}
	}

	// Try to load from legacy dev.yaml config file
	if _, err := os.Stat("configs/dev.yaml"); err == nil {
		data, err := os.ReadFile("configs/dev.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Override with environment variables
	applyEnvOverrides(config)

	return config, nil
}

// applyEnvOverrides applies environment variable overrides
func applyEnvOverrides(config *Config) {
	// Server config
	if addr := os.Getenv("SERVER_ADDRESS"); addr != "" {
		config.Server.Address = addr
	}
	if mode := os.Getenv("SERVER_MODE"); mode != "" {
		config.Server.Mode = mode
	}

	// Database config
	if host := os.Getenv("DB_HOST"); host != "" {
		config.Database.Host = host
	}
	if port := os.Getenv("DB_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &config.Database.Port)
	}
	if user := os.Getenv("DB_USER"); user != "" {
		config.Database.User = user
	}
	if pass := os.Getenv("DB_PASSWORD"); pass != "" {
		config.Database.Password = pass
	}
	if db := os.Getenv("DB_NAME"); db != "" {
		config.Database.Database = db
	}

	// JWT config
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		config.JWT.Secret = secret
	}

	// Kubernetes config
	if mode := os.Getenv("K8S_MODE"); mode != "" {
		config.Kubernetes.Mode = mode
	}
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		config.Kubernetes.OutOfCluster.Kubeconfig = kubeconfig
	}
	if kubeconfig := os.Getenv("K8S_KUBECONFIG"); kubeconfig != "" {
		config.Kubernetes.OutOfCluster.Kubeconfig = kubeconfig
	}
	if namespace := os.Getenv("K8S_NAMESPACE"); namespace != "" {
		config.Kubernetes.Common.Namespace = namespace
	}
	if storageClass := os.Getenv("K8S_STORAGE_CLASS"); storageClass != "" {
		config.Kubernetes.Common.StorageClass = storageClass
	}
	if hostPathPrefix := os.Getenv("K8S_PV_HOST_PATH_PREFIX"); hostPathPrefix != "" {
		config.Kubernetes.Runtime.PVC.HostPathPrefix = hostPathPrefix
	}

	if endpoint := os.Getenv("OBJECT_STORAGE_ENDPOINT"); endpoint != "" {
		config.ObjectStorage.Endpoint = endpoint
	}
	if region := os.Getenv("OBJECT_STORAGE_REGION"); region != "" {
		config.ObjectStorage.Region = region
	}
	if accessKey := os.Getenv("OBJECT_STORAGE_ACCESS_KEY"); accessKey != "" {
		config.ObjectStorage.AccessKey = accessKey
	}
	if secretKey := os.Getenv("OBJECT_STORAGE_SECRET_KEY"); secretKey != "" {
		config.ObjectStorage.SecretKey = secretKey
	}
	if bucket := os.Getenv("OBJECT_STORAGE_BUCKET"); bucket != "" {
		config.ObjectStorage.Bucket = bucket
	}
	if useSSL := os.Getenv("OBJECT_STORAGE_USE_SSL"); useSSL != "" {
		config.ObjectStorage.UseSSL = strings.EqualFold(useSSL, "true")
	}
	if basePath := os.Getenv("OBJECT_STORAGE_BASE_PATH"); basePath != "" {
		config.ObjectStorage.BasePath = basePath
	}
	if forcePathStyle := os.Getenv("OBJECT_STORAGE_FORCE_PATH_STYLE"); forcePathStyle != "" {
		config.ObjectStorage.ForcePathStyle = strings.EqualFold(forcePathStyle, "true")
	}
	if localFallback := os.Getenv("OBJECT_STORAGE_LOCAL_FALLBACK"); localFallback != "" {
		config.ObjectStorage.LocalFallback = localFallback
	}
	if baseURL := os.Getenv("SKILL_SCANNER_BASE_URL"); baseURL != "" {
		config.SkillScanner.BaseURL = baseURL
	}
	if apiKey := os.Getenv("SKILL_SCANNER_API_KEY"); apiKey != "" {
		config.SkillScanner.APIKey = apiKey
	}
	if enabled := os.Getenv("SKILL_SCANNER_ENABLED"); enabled != "" {
		config.SkillScanner.Enabled = strings.EqualFold(enabled, "true")
	}
	if timeoutSeconds := os.Getenv("SKILL_SCANNER_TIMEOUT_SECONDS"); timeoutSeconds != "" {
		fmt.Sscanf(timeoutSeconds, "%d", &config.SkillScanner.TimeoutSeconds)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetKubeconfigPath returns the kubeconfig path for out-of-cluster mode
func (c *Config) GetKubeconfigPath() string {
	return c.Kubernetes.OutOfCluster.Kubeconfig
}

// GetNamespace returns the namespace prefix
func (c *Config) GetNamespace() string {
	return c.Kubernetes.Common.Namespace
}

// GetStorageClass returns the storage class
func (c *Config) GetStorageClass() string {
	return c.Kubernetes.Common.StorageClass
}

// GetHostPathPrefix returns the host path prefix for PV creation
func (c *Config) GetHostPathPrefix() string {
	if c.Kubernetes.Runtime.PVC.HostPathPrefix != "" {
		return c.Kubernetes.Runtime.PVC.HostPathPrefix
	}
	return "/data/clawreef"
}

// GetMode returns the K8s connection mode
func (c *Config) GetMode() string {
	return c.Kubernetes.Mode
}
