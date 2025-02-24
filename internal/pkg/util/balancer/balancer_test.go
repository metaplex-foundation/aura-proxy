package balancer

import (
	"math"
	"sync"
	"testing"
)

func TestRoundRobin_GetNext_Concurrency(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	rr := NewRoundRobin(targets)

	numGoroutines := 100
	requestsPerGoroutine := 1000
	totalRequests := numGoroutines * requestsPerGoroutine

	// Use a map to count how many times each target is selected.
	counts := make(map[string]int)
	var countsMutex sync.Mutex

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				target, _, err := rr.GetNext(nil)
				if err != nil {
					t.Error(err)
					return
				}
				countsMutex.Lock()
				counts[target]++
				countsMutex.Unlock()
			}
		}()
	}

	wg.Wait()

	// Calculate the expected count for each target.
	expectedCount := float64(totalRequests) / float64(len(targets))

	// Check if the actual counts are within an acceptable tolerance (e.g., 1%) of the expected count.
	tolerance := 0.01
	for target, count := range counts {
		deviation := float64(count)/expectedCount - 1.0
		if deviation < -tolerance || deviation > tolerance {
			t.Errorf("Target %s: expected count ≈ %f, got %d (deviation %f)", target, expectedCount, count, deviation)
		}
	}
}

func TestRoundRobin_GetNext_Empty(t *testing.T) {
	rr := NewRoundRobin[string]([]string{})
	target, _, err := rr.GetNext(nil)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
	if target != "" {
		t.Errorf("Expected empty string, got %s", target)
	}
}

func TestRoundRobin_GetByCounter(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	rr := NewRoundRobin(targets)

	// Test various counter values
	testCases := []struct {
		counter  int
		expected string
	}{
		{0, "target1"},
		{1, "target2"},
		{2, "target3"},
		{3, "target1"}, // Wrap around
		{4, "target2"},
		{5, "target3"},
		// TODO: uncomment when negative values are supported
		// {-1, "target3"}, //test negative values
		// {-4, "target3"},
	}

	for _, tc := range testCases {
		actual, _, err := rr.GetNext(nil)
		if err != nil {
			t.Errorf("Error getting next target: %v", err)
		}
		if actual != tc.expected {
			t.Errorf("GetNext(): expected %s, got %s", tc.expected, actual)
		}
	}
}

func TestRoundRobin_GetCounter(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	rr := NewRoundRobin(targets)

	// Initial counter value should be 0
	if rr.GetCounter() != 0 {
		t.Errorf("Expected initial counter to be 0, got %d", rr.GetCounter())
	}

	// Increment counter and check
	rr.IncCounter()
	if rr.GetCounter() != 1 {
		t.Errorf("Expected counter to be 1, got %d", rr.GetCounter())
	}
}

func TestRoundRobin_GetTargetsCount(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	rr := NewRoundRobin(targets)
	if rr.GetTargetsCount() != len(targets) {
		t.Errorf("Expected GetTargetsCount to be %d, got %d", len(targets), rr.GetTargetsCount())
	}

	rr = NewRoundRobin([]string{})
	if rr.GetTargetsCount() != 0 {
		t.Errorf("Expected GetTargetsCount to be 0, got %d", rr.GetTargetsCount())
	}
}

func TestProbabilisticBalancer_GetNext(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	weights := []float64{0.5, 0.3, 0.2} // 50%, 30%, 20% distribution
	balancer, err := NewProbabilisticBalancer(targets, weights)
	if err != nil {
		t.Fatalf("Error creating balancer: %v", err)
	}

	numIterations := 100000
	counts := make(map[string]int)
	for i := 0; i < numIterations; i++ {
		target, _, err := balancer.GetNext(nil)
		if err != nil {
			t.Fatalf("Error getting next target: %v", err)
		}
		counts[target]++
	}

	// Check if the distribution is within an acceptable tolerance (e.g., 1%).
	tolerance := 0.02
	for i, target := range targets {
		expectedRatio := weights[i]
		actualRatio := float64(counts[target]) / float64(numIterations)
		deviation := math.Abs(actualRatio - expectedRatio)
		if deviation > tolerance {
			t.Errorf("Target %s: expected ratio ≈ %f, got %f (deviation %f)", target, expectedRatio, actualRatio, deviation)
		}
	}

	// Test with exclusions.
	exclude := []int{0} // Exclude target1
	counts = make(map[string]int)
	for i := 0; i < numIterations; i++ {
		target, _, err := balancer.GetNext(exclude)
		if err != nil {
			t.Fatalf("Error getting next target: %v", err)
		}
		counts[target]++
	}

	// Check distribution with exclusion.
	expectedCount2 := float64(numIterations) * (weights[1] / (weights[1] + weights[2]))
	expectedCount3 := float64(numIterations) * (weights[2] / (weights[1] + weights[2]))
	actualCount2 := float64(counts["target2"])
	actualCount3 := float64(counts["target3"])
	deviation2 := math.Abs(actualCount2-expectedCount2) / expectedCount2
	deviation3 := math.Abs(actualCount3-expectedCount3) / expectedCount3

	if deviation2 > tolerance {
		t.Errorf("Target %s: expected count ≈ %f, got %f (deviation %f)", "target2", expectedCount2, actualCount2, deviation2)
	}
	if deviation3 > tolerance {
		t.Errorf("Target %s: expected count ≈ %f, got %f (deviation %f)", "target3", expectedCount3, actualCount3, deviation3)
	}

	// Test all excluded
	exclude = []int{0, 1, 2}
	_, _, err = balancer.GetNext(exclude)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
}

func TestProbabilisticBalancer_GetNext_Exclusion(t *testing.T) {
	targets := []string{"target1", "target2", "target3"}
	weights := []float64{0.5, 0.3, 0.2}
	balancer, err := NewProbabilisticBalancer(targets, weights)
	if err != nil {
		t.Fatalf("Error creating balancer: %v", err)
	}
	numIterations := 1000
	for i := 0; i < numIterations; i++ {
		// Exclude target1 (index 0)
		target, index, err := balancer.GetNext([]int{0})
		if err != nil {
			t.Fatalf("Error getting next target: %v", err)
		}
		if target == "target1" {
			t.Errorf("Expected target1 to be excluded, but got %s", target)
		}
		if index == 0 {
			t.Errorf("Expected index 0 to be excluded, but got %d", index)
		}
	}

	// Exclude all targets
	_, _, err = balancer.GetNext([]int{0, 1, 2})
	if err == nil {
		t.Errorf("Expected error when all targets are excluded, but got nil")
	}
}

func TestProbabilisticBalancer_NewProbabilisticBalancer_Errors(t *testing.T) {
	// Mismatched targets and weights
	_, err := NewProbabilisticBalancer([]string{"target1"}, []float64{0.5, 0.5})
	if err == nil {
		t.Errorf("Expected error for mismatched targets and weights, but got nil")
	}

	// No targets
	_, err = NewProbabilisticBalancer([]string{}, []float64{})
	if err == nil {
		t.Errorf("Expected error for no targets, but got nil")
	}

	// Negative weight
	_, err = NewProbabilisticBalancer([]string{"target1"}, []float64{-0.5})
	if err == nil {
		t.Errorf("Expected error for negative weight, but got nil")
	}

	// Zero total weight
	_, err = NewProbabilisticBalancer([]string{"target1", "target2"}, []float64{0, 0})
	if err == nil {
		t.Errorf("Expected error for zero total weight, but got nil")
	}
}

func TestProbabilisticBalancer_IsAvailable(t *testing.T) {
	balancer, err := NewProbabilisticBalancer([]string{"target1"}, []float64{1.0})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !balancer.IsAvailable() {
		t.Errorf("Expected IsAvailable to be true, got false")
	}

	balancer, err = NewProbabilisticBalancer([]string{}, []float64{})
	if err == nil {
		t.Errorf("Expected an error but didn't get one")
	}
}

func TestProbabilisticBalancer_GetTargetsCount(t *testing.T) {
	targets := []string{"a", "b", "c"}
	weights := []float64{0.2, 0.3, 0.5}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	count := balancer.GetTargetsCount()
	if count != len(targets) {
		t.Errorf("Expected GetTargetsCount to return %d, but got %d", len(targets), count)
	}
}

func BenchmarkProbabilisticBalancer_GetNext_2Targets_NoExclusions(b *testing.B) {
	targets := []string{"target1", "target2"}
	weights := []float64{0.5, 0.5}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	b.ResetTimer() // Reset timer to exclude setup time
	for i := 0; i < b.N; i++ {
		_, _, _ = balancer.GetNext(nil)
	}
}

func BenchmarkProbabilisticBalancer_GetNext_2Targets_1Exclusion(b *testing.B) {
	targets := []string{"target1", "target2"}
	weights := []float64{0.5, 0.5}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	exclude := []int{0} // Exclude the first target
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = balancer.GetNext(exclude)
	}
}

func BenchmarkProbabilisticBalancer_GetNext_3Targets_NoExclusions(b *testing.B) {
	targets := []string{"target1", "target2", "target3"}
	weights := []float64{0.3, 0.3, 0.4}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = balancer.GetNext(nil)
	}
}

func BenchmarkProbabilisticBalancer_GetNext_3Targets_1Exclusion(b *testing.B) {
	targets := []string{"target1", "target2", "target3"}
	weights := []float64{0.3, 0.3, 0.4}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	exclude := []int{1} // Exclude the second target
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = balancer.GetNext(exclude)
	}
}
func BenchmarkRoundRobinBalancer_GetNext_3Targets_NoExclusions(b *testing.B) {
	targets := []string{"target1", "target2", "target3"}
	balancer := NewRoundRobin(targets)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = balancer.GetNext(nil)
	}
}

func BenchmarkProbabilisticBalancer_GetNext_Concurrent(b *testing.B) {
	targets := []string{"target1", "target2", "target3"}
	weights := []float64{0.3, 0.3, 0.4}
	balancer, _ := NewProbabilisticBalancer(targets, weights)
	numGoroutines := 16 // Adjust this number to simulate different levels of concurrency
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for j := 0; j < numGoroutines; j++ {
			go func() {
				defer wg.Done()
				_, _, _ = balancer.GetNext(nil) // Or test with exclusions: []int{j % len(targets)}
			}()
		}
		wg.Wait()
	}
}

func BenchmarkRoundRobinBalancer_GetNext_Concurrent(b *testing.B) {
	targets := []string{"target1", "target2", "target3"}
	balancer := NewRoundRobin(targets)
	numGoroutines := 16 // Adjust this number to simulate different levels of concurrency
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for j := 0; j < numGoroutines; j++ {
			go func() {
				defer wg.Done()
				_, _, _ = balancer.GetNext(nil)
			}()
		}
		wg.Wait()
	}
}
