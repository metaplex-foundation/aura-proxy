package solana

import (
	"fmt"
	"sync"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util/balancer"
)

const (
	// DefaultEndpointWeight is the default weight for endpoints without a specified weight
	DefaultEndpointWeight = 1.0
)

// methodTargetInfo holds information about targets for a specific method
type methodTargetInfo struct {
	targets []*ProxyTarget
	weights []float64

	// The balancer for this method
	balancer balancer.TargetSelector[*ProxyTarget]
}

// MethodBasedRouter implements the MethodRouter interface
type MethodBasedRouter struct {
	// Maps method names to their target selectors
	methodMap map[string]*methodTargetInfo

	// Default selector for methods not explicitly mapped
	defaultTargetInfo *methodTargetInfo

	// WebSocket selector for handling WebSocket connections
	wsTargetInfo *methodTargetInfo

	// All providers configured in the system
	providers map[string][]*ProxyTarget

	// All method groups defined in the config
	methodGroups map[string][]string

	// Set of all methods explicitly handled by this router (using struct{} for memory efficiency)
	supportedMethods map[string]struct{}

	mutex sync.RWMutex
}

// NewMethodBasedRouter creates a new method-based router from the given configuration
func NewMethodBasedRouter(cfg *configtypes.SolanaConfig) (*MethodBasedRouter, error) {
	router := &MethodBasedRouter{
		methodMap:        make(map[string]*methodTargetInfo),
		providers:        make(map[string][]*ProxyTarget),
		methodGroups:     make(map[string][]string),
		supportedMethods: make(map[string]struct{}),
	}

	// Process method groups
	for _, group := range cfg.MethodGroups {
		router.methodGroups[group.Name] = group.Methods
	}

	// Process provider configurations
	if err := router.processProviders(cfg.Providers); err != nil {
		return nil, fmt.Errorf("processing providers: %w", err)
	}

	// Handle backward compatibility
	if err := router.processLegacyConfig(cfg); err != nil {
		return nil, fmt.Errorf("processing legacy config: %w", err)
	}

	return router, nil
}

// processProviders processes the provider configurations and builds the method routing table
func (r *MethodBasedRouter) processProviders(providers []configtypes.ProviderConfig) error {
	for _, provider := range providers {
		// Create targets for all endpoints in this provider
		var providerTargets []*ProxyTarget

		for _, endpoint := range provider.Endpoints {
			target := NewProxyTarget(
				models.URLWithMethods{URL: endpoint.URL},
				0, // reqLimit
				provider.Name,
				endpoint.NodeType,
			)
			providerTargets = append(providerTargets, target)

			// First, expand method groups into concrete methods
			var expandedMethods []string

			// Process method groups
			for _, groupName := range endpoint.MethodGroups {
				if methods, ok := r.methodGroups[groupName]; ok {
					expandedMethods = append(expandedMethods, methods...)
				} else {
					log.Logger.Proxy.Warnf("Method group '%s' referenced but not defined", groupName)
				}
			}

			// Add explicitly specified methods
			expandedMethods = append(expandedMethods, endpoint.Methods...)

			// Create a weight value (default to 1.0 if not specified)
			weight := endpoint.Weight
			if weight <= 0 {
				weight = DefaultEndpointWeight
			}

			// Add this target to the method map for each supported method
			for _, method := range expandedMethods {
				// Skip if method is in exclude list
				if contains(endpoint.ExcludeMethods, method) {
					continue
				}

				// Get or create methodTargetInfo for this method
				info, exists := r.methodMap[method]
				if !exists {
					info = &methodTargetInfo{}
					r.methodMap[method] = info
				}

				// Add target and weight
				info.targets = append(info.targets, target)
				info.weights = append(info.weights, weight)

				// Mark method as supported
				r.supportedMethods[method] = struct{}{}
			}

			// Handle WebSocket connections
			if endpoint.HandleWebSocket {
				// Create wsTargetInfo if it doesn't exist
				if r.wsTargetInfo == nil {
					r.wsTargetInfo = &methodTargetInfo{}
				}

				// Add target and weight to WebSocket handler
				r.wsTargetInfo.targets = append(r.wsTargetInfo.targets, target)
				r.wsTargetInfo.weights = append(r.wsTargetInfo.weights, weight)
			}

			// Handle "other" methods
			if endpoint.HandleOther {
				// Create defaultTargetInfo if it doesn't exist
				if r.defaultTargetInfo == nil {
					r.defaultTargetInfo = &methodTargetInfo{}
				}

				// Add target and weight to default handler
				r.defaultTargetInfo.targets = append(r.defaultTargetInfo.targets, target)
				r.defaultTargetInfo.weights = append(r.defaultTargetInfo.weights, weight)
			}
		}

		// Store all targets for this provider
		r.providers[provider.Name] = providerTargets
	}

	// Create balancers for each method
	for method, info := range r.methodMap {
		if len(info.targets) > 0 {
			balancer, err := balancer.NewProbabilisticBalancer(info.targets, info.weights)
			if err != nil {
				return fmt.Errorf("creating balancer for method %s: %w", method, err)
			}
			info.balancer = balancer
		}
	}

	// Create balancer for WebSocket target info if it exists
	if r.wsTargetInfo != nil && len(r.wsTargetInfo.targets) > 0 {
		balancer, err := balancer.NewProbabilisticBalancer(
			r.wsTargetInfo.targets,
			r.wsTargetInfo.weights,
		)
		if err != nil {
			return fmt.Errorf("creating balancer for WebSocket connections: %w", err)
		}
		r.wsTargetInfo.balancer = balancer
	}

	// Create balancer for default target info if it exists
	if r.defaultTargetInfo != nil && len(r.defaultTargetInfo.targets) > 0 {
		balancer, err := balancer.NewProbabilisticBalancer(
			r.defaultTargetInfo.targets,
			r.defaultTargetInfo.weights,
		)
		if err != nil {
			return fmt.Errorf("creating balancer for default routing: %w", err)
		}
		r.defaultTargetInfo.balancer = balancer
	}

	return nil
}

// processBatchNodes is a helper function to process a batch of nodes into targets
// and create a balancer from them.
func (r *MethodBasedRouter) processBatchNodes(
	nodes []configtypes.SolanaNode,
	methodsToAdd map[string]struct{},
) ([]*ProxyTarget, []float64, error) {
	var targets []*ProxyTarget
	var weights []float64

	for _, node := range nodes {
		target := NewProxyTarget(
			models.URLWithMethods{URL: node.URL.String()},
			0,
			node.Provider,
			node.NodeType,
		)
		targets = append(targets, target)
		weights = append(weights, DefaultEndpointWeight) // Default weight

		// Add this provider to our providers map
		r.providers[node.Provider] = append(r.providers[node.Provider], target)
	}

	// Mark methods as supported
	for method := range methodsToAdd {
		if len(targets) > 0 {
			balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
			if err != nil {
				return nil, nil, fmt.Errorf("creating balancer for method %s: %w", method, err)
			}
			info := &methodTargetInfo{
				targets:  targets,
				weights:  weights,
				balancer: balancer,
			}
			r.methodMap[method] = info
		}
		r.supportedMethods[method] = struct{}{}
	}

	return targets, weights, nil
}

// processLegacyConfig handles backward compatibility with old configuration format
func (r *MethodBasedRouter) processLegacyConfig(cfg *configtypes.SolanaConfig) error {
	// Process DasAPINodes as handlers for CNFT methods
	if len(cfg.DasAPINodes) > 0 {
		// Create a set of CNFT methods
		cnftMethods := make(map[string]struct{})
		for method := range solana.CNFTMethodList {
			cnftMethods[method] = struct{}{}
		}

		_, _, err := r.processBatchNodes(cfg.DasAPINodes, cnftMethods)
		if err != nil {
			return fmt.Errorf("processing DAS nodes: %w", err)
		}
	}

	// Process WSHostNodes as handlers for WebSocket connections
	if len(cfg.WSHostNodes) > 0 {
		// Create an empty set as the websocket method is handled by the router
		wsMethods := map[string]struct{}{}

		targets, weights, err := r.processBatchNodes(cfg.WSHostNodes, wsMethods)
		if err != nil {
			return fmt.Errorf("processing WebSocket nodes: %w", err)
		}

		// Create WebSocket target info
		if len(targets) > 0 {
			balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
			if err != nil {
				return fmt.Errorf("creating balancer for WebSocket nodes: %w", err)
			}
			wsInfo := &methodTargetInfo{
				targets:  targets,
				weights:  weights,
				balancer: balancer,
			}

			// Store as wsTargetInfo for direct access
			r.wsTargetInfo = wsInfo
		}
	}

	// Process BasicRouteNodes as default routes
	if len(cfg.BasicRouteNodes) > 0 {
		// No specific methods to add for basic route nodes
		emptyMethods := make(map[string]struct{})

		targets, weights, err := r.processBatchNodes(cfg.BasicRouteNodes, emptyMethods)
		if err != nil {
			return fmt.Errorf("processing basic route nodes: %w", err)
		}

		// Create default target info
		if len(targets) > 0 {
			balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
			if err != nil {
				return fmt.Errorf("creating balancer for basic route nodes: %w", err)
			}
			r.defaultTargetInfo = &methodTargetInfo{
				targets:  targets,
				weights:  weights,
				balancer: balancer,
			}
		}
	}

	return nil
}

// GetBalancerForMethod returns the appropriate balancer for the given method
func (r *MethodBasedRouter) GetBalancerForMethod(method string) (balancer.TargetSelector[*ProxyTarget], bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Try to find a specific method balancer
	if info, ok := r.methodMap[method]; ok && info.balancer != nil {
		return info.balancer, true
	}

	// Fall back to default balancer
	if r.defaultTargetInfo != nil && r.defaultTargetInfo.balancer != nil {
		return r.defaultTargetInfo.balancer, true
	}

	return nil, false
}

// UpdateTargetStats updates the stats for a target after a request
func (r *MethodBasedRouter) UpdateTargetStats(target *ProxyTarget, success bool, methods []string, responseTimeMs, slotAmount int64) {
	if target == nil {
		return
	}

	target.UpdateStats(success, methods, responseTimeMs, slotAmount)
}

// IsMethodSupported checks if a method is supported by this router
func (r *MethodBasedRouter) IsMethodSupported(method string) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Check if the method is explicitly supported
	_, ok := r.supportedMethods[method]
	if ok {
		return true
	}

	// Check if we have a default handler
	return r.defaultTargetInfo != nil && r.defaultTargetInfo.balancer != nil && r.defaultTargetInfo.balancer.IsAvailable()
}

// IsAvailable checks if there are any available targets
func (r *MethodBasedRouter) IsAvailable() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// Check if WebSocket target is available
	if r.wsTargetInfo != nil && r.wsTargetInfo.balancer != nil && r.wsTargetInfo.balancer.IsAvailable() {
		return true
	}

	// Check if any method-specific target is available
	for _, info := range r.methodMap {
		if info.balancer != nil && info.balancer.IsAvailable() {
			return true
		}
	}

	// Check if default target is available
	return r.defaultTargetInfo != nil && r.defaultTargetInfo.balancer != nil && r.defaultTargetInfo.balancer.IsAvailable()
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
