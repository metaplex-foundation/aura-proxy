package solana

import (
	"fmt"
	"sync"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/configtypes"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util/balancer"
	echoUtil "aura-proxy/internal/pkg/util/echo"
)

// Special method name for WebSocket connections
const WebSocketMethodName = "websocket:connect"

// MethodRouter is responsible for routing requests to appropriate targets based on the requested method
type MethodRouter interface {
	// GetTargetForMethods returns the appropriate target for the given methods
	GetTargetForMethods(methods []string, c *echoUtil.CustomContext) (*ProxyTarget, error)
	
	// GetBalancerForMethod returns the appropriate balancer for the given method
	// This allows the caller to handle target selection with exclude functionality
	GetBalancerForMethod(method string) (balancer.TargetSelector[*ProxyTarget], bool)
	
	// UpdateTargetStats updates the stats for a target after a request
	UpdateTargetStats(target *ProxyTarget, success bool, methods []string, responseTimeMs, slotAmount int64)
	
	// IsMethodSupported checks if a method is supported by this router
	IsMethodSupported(method string) bool
	
	// IsAvailable checks if there are any available targets
	IsAvailable() bool
}

// methodTargetInfo holds information about a target for a specific method
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
	
	// Set of all methods explicitly handled by this router
	supportedMethods map[string]bool
	
	mutex sync.RWMutex
}

// NewMethodBasedRouter creates a new method-based router from the given configuration
func NewMethodBasedRouter(cfg *configtypes.SolanaConfig) (*MethodBasedRouter, error) {
	router := &MethodBasedRouter{
		methodMap:        make(map[string]*methodTargetInfo),
		providers:        make(map[string][]*ProxyTarget),
		methodGroups:     make(map[string][]string),
		supportedMethods: make(map[string]bool),
	}
	
	// Process method groups
	for _, group := range cfg.MethodGroups {
		router.methodGroups[group.Name] = group.Methods
	}
	
	// Process provider configurations
	if err := router.processProviders(cfg.Providers); err != nil {
		return nil, err
	}
	
	// Handle backward compatibility
	if err := router.processLegacyConfig(cfg); err != nil {
		return nil, err
	}
	
	return router, nil
}

// processProviders processes the provider configurations and builds the method routing table
func (r *MethodBasedRouter) processProviders(providers []configtypes.ProviderConfig) error {
	for _, provider := range providers {
		// Create targets for all endpoints in this provider
		providerTargets := make([]*ProxyTarget, 0, len(provider.Endpoints))
		
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
				weight = 1.0
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
					info = &methodTargetInfo{
						targets: make([]*ProxyTarget, 0),
						weights: make([]float64, 0),
					}
					r.methodMap[method] = info
				}
				
				// Add target and weight
				info.targets = append(info.targets, target)
				info.weights = append(info.weights, weight)
				
				// Mark method as supported
				r.supportedMethods[method] = true
			}
			
			// Handle "other" methods
			if endpoint.HandleOther {
				// Create defaultTargetInfo if it doesn't exist
				if r.defaultTargetInfo == nil {
					r.defaultTargetInfo = &methodTargetInfo{
						targets: make([]*ProxyTarget, 0),
						weights: make([]float64, 0),
					}
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

// processLegacyConfig handles backward compatibility with old configuration format
func (r *MethodBasedRouter) processLegacyConfig(cfg *configtypes.SolanaConfig) error {
	// Process DasAPINodes as handlers for CNFT methods
	if len(cfg.DasAPINodes) > 0 {
		targets := make([]*ProxyTarget, 0, len(cfg.DasAPINodes))
		weights := make([]float64, 0, len(cfg.DasAPINodes))
		
		for _, node := range cfg.DasAPINodes {
			target := NewProxyTarget(
				models.URLWithMethods{URL: node.URL.String()},
				0,
				node.Provider,
				node.NodeType,
			)
			targets = append(targets, target)
			weights = append(weights, 1.0) // Default weight
			
			// Add this provider to our providers map
			r.providers[node.Provider] = append(r.providers[node.Provider], target)
		}
		
		// Add targets for each CNFT method
		for method := range solana.CNFTMethodList {
			info := &methodTargetInfo{
				targets: targets,
				weights: weights,
			}
			
			balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
			if err != nil {
				return fmt.Errorf("creating balancer for CNFT method %s: %w", method, err)
			}
			info.balancer = balancer
			
			r.methodMap[method] = info
			r.supportedMethods[method] = true
		}
	}
	
	// Process WSHostNodes as handlers for WebSocket connections
	if len(cfg.WSHostNodes) > 0 {
		targets := make([]*ProxyTarget, 0, len(cfg.WSHostNodes))
		weights := make([]float64, 0, len(cfg.WSHostNodes))
		
		for _, node := range cfg.WSHostNodes {
			target := NewProxyTarget(
				models.URLWithMethods{URL: node.URL.String()},
				0,
				node.Provider,
				node.NodeType,
			)
			targets = append(targets, target)
			weights = append(weights, 1.0) // Default weight
			
			// Add this provider to our providers map
			r.providers[node.Provider] = append(r.providers[node.Provider], target)
		}
		
		// Create WebSocket target info
		wsInfo := &methodTargetInfo{
			targets: targets,
			weights: weights,
		}
		
		balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
		if err != nil {
			return fmt.Errorf("creating balancer for WebSocket nodes: %w", err)
		}
		wsInfo.balancer = balancer
		
		// Add to methodMap with special WebSocket method name
		r.methodMap[WebSocketMethodName] = wsInfo
		r.supportedMethods[WebSocketMethodName] = true
		
		// Save as wsTargetInfo for direct access
		r.wsTargetInfo = wsInfo
	}
	
	// Process BasicRouteNodes as default routes
	if len(cfg.BasicRouteNodes) > 0 {
		targets := make([]*ProxyTarget, 0, len(cfg.BasicRouteNodes))
		weights := make([]float64, 0, len(cfg.BasicRouteNodes))
		
		for _, node := range cfg.BasicRouteNodes {
			target := NewProxyTarget(
				models.URLWithMethods{URL: node.URL.String()},
				0,
				node.Provider,
				node.NodeType,
			)
			targets = append(targets, target)
			weights = append(weights, 1.0) // Default weight
			
			// Add this provider to our providers map
			r.providers[node.Provider] = append(r.providers[node.Provider], target)
		}
		
		// Create default target info
		r.defaultTargetInfo = &methodTargetInfo{
			targets: targets,
			weights: weights,
		}
		
		balancer, err := balancer.NewProbabilisticBalancer(targets, weights)
		if err != nil {
			return fmt.Errorf("creating balancer for basic route nodes: %w", err)
		}
		r.defaultTargetInfo.balancer = balancer
	}
	
	return nil
}

// GetTargetForMethods returns the appropriate target for the given methods
func (r *MethodBasedRouter) GetTargetForMethods(methods []string, c *echoUtil.CustomContext) (*ProxyTarget, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	
	// Try to find a target for each method
	for _, method := range methods {
		if info, ok := r.methodMap[method]; ok && info.balancer != nil && info.balancer.IsAvailable() {
			target, _, err := info.balancer.GetNext(nil)
			if err == nil && target != nil {
				return target, nil
			}
		}
	}
	
	// Fall back to default target info
	if r.defaultTargetInfo != nil && r.defaultTargetInfo.balancer != nil && r.defaultTargetInfo.balancer.IsAvailable() {
		target, _, err := r.defaultTargetInfo.balancer.GetNext(nil)
		if err == nil && target != nil {
			return target, nil
		}
	}
	
	return nil, fmt.Errorf("no target available for methods: %v", methods)
}

// GetBalancerForMethod returns the appropriate balancer for the given method
func (r *MethodBasedRouter) GetBalancerForMethod(method string) (balancer.TargetSelector[*ProxyTarget], bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	
	// Check for WebSocket method
	if method == WebSocketMethodName && r.wsTargetInfo != nil && r.wsTargetInfo.balancer != nil {
		return r.wsTargetInfo.balancer, true
	}
	
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
	
	// Check if it's a WebSocket request
	if method == WebSocketMethodName {
		return r.wsTargetInfo != nil && r.wsTargetInfo.balancer != nil && r.wsTargetInfo.balancer.IsAvailable()
	}
	
	// Check if the method is explicitly supported
	if r.supportedMethods[method] {
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