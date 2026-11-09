package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
)

const (
	providerDiscoveryCacheTTL   = 5 * time.Minute
	providerDiscoveryFailureTTL = 30 * time.Second
)

// ExpandedLLMModelCatalog exposes the active model catalogue after each
// configured provider has been expanded through its discovery endpoint.
type ExpandedLLMModelCatalog interface {
	ListExpandedActiveModels() ([]models.LLMModel, error)
}

type providerModelGroup struct {
	representative models.LLMModel
	items          []models.LLMModel
}

type cachedProviderDiscovery struct {
	items        []DiscoveredLLMModel
	errorMessage string
	expiresAt    time.Time
}

type providerDiscoveryCache struct {
	mu      sync.Mutex
	entries map[string]cachedProviderDiscovery
}

func newProviderDiscoveryCache() *providerDiscoveryCache {
	return &providerDiscoveryCache{entries: make(map[string]cachedProviderDiscovery)}
}

// ListExpandedActiveModels retains the upstream catalog interface, but only
// returns explicitly configured deployments. Discovery remains an admin-only
// suggestion; its results do not grant permission to call additional models.
func (s *llmModelService) ListExpandedActiveModels() ([]models.LLMModel, error) {
 items, err := s.ListActiveModels()
 if err != nil { return nil, err }
 for i := range items {
  if items[i].CatalogProviderName == "" { items[i].CatalogProviderName = modelCatalogProviderName(items[i]) }
 }
 return items, nil
}

func groupModelsByProvider(items []models.LLMModel) ([]providerModelGroup, map[string]struct{}) {
	groups := make([]providerModelGroup, 0, len(items))
	groupIndexes := make(map[string]int, len(items))
	usedDisplayNames := make(map[string]struct{}, len(items))
	for _, item := range items {
		if displayName := strings.TrimSpace(item.DisplayName); displayName != "" {
			usedDisplayNames[strings.ToLower(displayName)] = struct{}{}
		}

		key := providerDiscoveryCacheKey(item)
		if index, exists := groupIndexes[key]; exists {
			groups[index].items = append(groups[index].items, item)
			if groups[index].representative.IsSecure && !item.IsSecure {
				groups[index].representative = item
			}
			continue
		}
		groupIndexes[key] = len(groups)
		groups = append(groups, providerModelGroup{
			representative: item,
			items:          []models.LLMModel{item},
		})
	}
	return groups, usedDisplayNames
}

func providerModelNameSet(items []models.LLMModel) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		if name := strings.ToLower(strings.TrimSpace(item.ProviderModelName)); name != "" {
			result[name] = struct{}{}
		}
	}
	return result
}

func modelCatalogProviderName(item models.LLMModel) string {
	name := firstNonEmptyCatalogValue(item.CatalogProviderName, item.DisplayName, item.ProviderType, "provider")
	// A slash separates the runtime provider name from its model id.
	name = strings.ReplaceAll(name, "/", "-")
	return strings.ReplaceAll(name, `\`, "-")
}

func uniqueDiscoveredModelAlias(item models.LLMModel, providerModelName string, used map[string]struct{}) string {
	alias := strings.TrimSpace(providerModelName)
	if _, exists := used[strings.ToLower(alias)]; !exists {
		used[strings.ToLower(alias)] = struct{}{}
		return alias
	}

	base := firstNonEmptyCatalogValue(item.DisplayName, item.ProviderType, "provider") + "/" + alias
	for suffix, candidate := 2, base; ; suffix++ {
		normalized := strings.ToLower(candidate)
		if _, exists := used[normalized]; !exists {
			used[normalized] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func (s *llmModelService) discoverProviderModelsCached(item models.LLMModel) ([]DiscoveredLLMModel, error) {
	key := providerDiscoveryCacheKey(item)
	now := time.Now()
	if cached, ok := s.discoveryCache.load(key, now); ok {
		if cached.errorMessage != "" {
			return nil, errors.New(cached.errorMessage)
		}
		return cached.items, nil
	}

	discovered, err := s.DiscoverProviderModels(DiscoverLLMModelsRequest{
		ProviderType:    item.ProviderType,
		ProtocolType:    item.ProtocolType,
		BaseURL:         item.BaseURL,
		APIKey:          item.APIKey,
		APIKeySecretRef: item.APIKeySecretRef,
	})
	ttl := providerDiscoveryCacheTTL
	cached := cachedProviderDiscovery{items: cloneDiscoveredModels(discovered)}
	if err != nil {
		ttl = providerDiscoveryFailureTTL
		cached.errorMessage = err.Error()
	}
	cached.expiresAt = now.Add(ttl)
	s.discoveryCache.store(key, cached)
	if err != nil {
		return nil, err
	}
	return cloneDiscoveredModels(discovered), nil
}

func providerDiscoveryCacheKey(item models.LLMModel) string {
	apiKey := ""
	if item.APIKey != nil {
		apiKey = strings.TrimSpace(*item.APIKey)
	}
	secretRef := ""
	if item.APIKeySecretRef != nil {
		secretRef = strings.TrimSpace(*item.APIKeySecretRef)
	}
	raw := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(item.ProviderType)),
		strings.ToLower(strings.TrimSpace(models.ResolveLLMProtocolTypeOrDefault(item.ProviderType, item.ProtocolType))),
		strings.ToLower(strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")),
		apiKey,
		secretRef,
	}, "\x00")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func (s *llmModelService) resetProviderDiscoveryCache() {
	if s.discoveryCache == nil {
		s.discoveryCache = newProviderDiscoveryCache()
		return
	}
	s.discoveryCache.reset()
}

func (c *providerDiscoveryCache) load(key string, now time.Time) (cachedProviderDiscovery, bool) {
	if c == nil {
		return cachedProviderDiscovery{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.entries[key]
	if !exists || !now.Before(entry.expiresAt) {
		return cachedProviderDiscovery{}, false
	}
	entry.items = cloneDiscoveredModels(entry.items)
	return entry, true
}

func (c *providerDiscoveryCache) store(key string, entry cachedProviderDiscovery) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedProviderDiscovery)
	}
	entry.items = cloneDiscoveredModels(entry.items)
	c.entries[key] = entry
}

func (c *providerDiscoveryCache) reset() {
	c.mu.Lock()
	c.entries = make(map[string]cachedProviderDiscovery)
	c.mu.Unlock()
}

func cloneDiscoveredModels(items []DiscoveredLLMModel) []DiscoveredLLMModel {
	return append([]DiscoveredLLMModel(nil), items...)
}
