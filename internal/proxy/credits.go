package proxy

import "fmt"

const unknownMethodCost = 1

func calculateCreditsCost(methodArray []string, methodCost map[string]uint) (credits uint, err error) {
	var missingMethods []string
	for _, m := range methodArray {
		cost, ok := methodCost[m]
		if !ok {
			cost += unknownMethodCost
			missingMethods = append(missingMethods, m)
		}
		credits += cost
	}

	if len(missingMethods) > 0 {
		return credits, fmt.Errorf("unable to calculate cost due to missing methods: %s", missingMethods)
	}

	return credits, nil
}
