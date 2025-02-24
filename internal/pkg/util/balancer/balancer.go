package balancer

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// TargetSelector interface abstracts the target selection logic.
type TargetSelector[T any] interface {
	GetNext(exclude []int) (T, int, error) // Returns target, index, and error
	IsAvailable() bool
	GetTargetsCount() int
}

// RoundRobin (existing implementation, modified to implement TargetSelector)
type RoundRobin[T comparable] struct {
	mx      *sync.Mutex
	targets []T
	counter int
}

func NewRoundRobin[T comparable](targets []T) *RoundRobin[T] {
	return &RoundRobin[T]{
		mx:      &sync.Mutex{},
		targets: targets,
	}
}

// GetNext implements the TargetSelector interface for RoundRobin.
func (r *RoundRobin[T]) GetNext(exclude []int) (t T, index int, err error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	if len(r.targets) == 0 {
		return t, -1, fmt.Errorf("no targets available")
	}

	// Simple round-robin, ignoring excludes (for now, in this basic implementation)
	index = r.counter
	r.counter = (r.counter + 1) % len(r.targets)
	t = r.targets[index]
	return t, index, nil
}

func (r *RoundRobin[T]) GetByCounter(counter int) (t T) {
	if len(r.targets) == 0 {
		return t
	}

	r.mx.Lock()
	counter %= len(r.targets)
	t = r.targets[counter]
	r.mx.Unlock()

	return
}

func (r *RoundRobin[T]) GetCounter() int {
	r.mx.Lock()
	defer r.mx.Unlock()
	return r.counter
}
func (r *RoundRobin[T]) IncCounter() {
	r.mx.Lock()
	defer r.mx.Unlock()
	r.counter++
}
func (r *RoundRobin[T]) IsAvailable() bool {
	return len(r.targets) != 0
}
func (r *RoundRobin[T]) GetTargetsCount() int {
	return len(r.targets)
}

// ProbabilisticBalancer
type ProbabilisticBalancer[T any] struct {
	targets           []T
	weights           []float64
	cumulativeWeights []float64
	r                 *rand.Rand // Use a dedicated random number generator
}

func NewProbabilisticBalancer[T any](targets []T, weights []float64) (*ProbabilisticBalancer[T], error) {
	if len(targets) != len(weights) {
		return nil, fmt.Errorf("number of targets (%d) must match number of weights (%d)", len(targets), len(weights))
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("must provide at least one target")
	}

	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("weights must be non-negative")
		}
	}

	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}
	if totalWeight == 0.0 {
		return nil, fmt.Errorf("total weight must be greater than zero")
	}

	// Normalize weights to sum up to 1.0
	normalizedWeights := make([]float64, len(weights))
	for i, w := range weights {
		normalizedWeights[i] = w / totalWeight
	}

	// Create cumulative weights array for efficient selection
	cumulativeWeights := make([]float64, len(normalizedWeights))
	cumulativeSum := 0.0
	for i, w := range normalizedWeights {
		cumulativeSum += w
		cumulativeWeights[i] = cumulativeSum
	}

	return &ProbabilisticBalancer[T]{
		targets:           targets,
		weights:           normalizedWeights, // Store normalized weights
		cumulativeWeights: cumulativeWeights,
		r:                 rand.New(rand.NewSource(time.Now().UnixNano())), // Initialize the random number generator
	}, nil
}

func (p *ProbabilisticBalancer[T]) GetNext(exclude []int) (t T, index int, err error) {
	if len(p.targets) == 0 {
		return t, -1, fmt.Errorf("no targets available")
	}

	// Fast path for no exclusions.
	if len(exclude) == 0 {
		randomValue := p.r.Float64()
		for i, cw := range p.cumulativeWeights {
			if randomValue <= cw {
				return p.targets[i], i, nil
			}
		}
		// Should not happen, but handle for safety.
		return t, -1, fmt.Errorf("internal error: no target selected")
	}

	// Sort the exclude slice for efficient lookup.
	sort.Ints(exclude)

	filteredIndices := make([]int, 0, len(p.targets)) // Pre-allocate with maximum possible size
	cumulativeSum := 0.0
	excludeIndex := 0 // Index for iterating through the sorted exclude slice
	cumulativeWeights := make([]float64, 0, len(p.targets))

	// Iterate through targets and build cumulative weights, skipping excluded targets.
	for i := range p.targets {
		// Check if the current target is excluded using the sorted exclude slice.
		if excludeIndex < len(exclude) && i == exclude[excludeIndex] {
			excludeIndex++ // Move to the next excluded index
			continue       // Skip this target
		}

		cumulativeSum += p.weights[i]
		filteredIndices = append(filteredIndices, i)
		cumulativeWeights = append(cumulativeWeights, cumulativeSum)
	}

	if len(filteredIndices) == 0 {
		return t, -1, fmt.Errorf("all targets excluded")
	}
	if cumulativeSum == 0 {
		return p.targets[filteredIndices[0]], filteredIndices[0], nil
	}

	randomValue := p.r.Float64() * cumulativeSum
	selectedOriginalIndex := -1
	for i, cumWeight := range cumulativeWeights {
		if i >= len(filteredIndices) {
			break
		}
		if randomValue <= cumWeight {
			selectedOriginalIndex = filteredIndices[i]
			break
		}
	}

	if selectedOriginalIndex == -1 {
		selectedOriginalIndex = filteredIndices[len(filteredIndices)-1]
	}

	return p.targets[selectedOriginalIndex], selectedOriginalIndex, nil
}

func (p *ProbabilisticBalancer[T]) IsAvailable() bool {
	return len(p.targets) > 0
}

func (p *ProbabilisticBalancer[T]) GetTargetsCount() int {
	return len(p.targets)
}
