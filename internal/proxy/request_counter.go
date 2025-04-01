package proxy

import (
	"context"
	"sync"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
)

const flushInterval = time.Second * 30

type RequestCounter struct {
	wg       *sync.WaitGroup
	auraAPI  auraProto.AuraClient
	counters map[string]map[string]map[string]map[string]*auraProto.RequestsWithUsage
	mx       sync.Mutex
}

func NewRequestCounter(ctx context.Context, wg *sync.WaitGroup, auraAPI auraProto.AuraClient) (r *RequestCounter) {
	r = &RequestCounter{
		wg:       wg,
		counters: make(map[string]map[string]map[string]map[string]*auraProto.RequestsWithUsage), // providerID/chain/requestType/token/reqCount
		mx:       sync.Mutex{},
		auraAPI:  auraAPI,
	}

	// err not emitted
	_ = util.AsyncRunWithInterval(ctx, wg, flushInterval, false, true, func(context.Context) error {
		err := r.flush() //nolint:contextcheck
		if err != nil {
			log.Logger.Proxy.Errorf("RequestCounter: flush: %s", err)
		}

		return nil
	})

	return r
}

func (r *RequestCounter) IncUserRequests(user *auraProto.UserWithTokens, creditsUsed int64, chain, token, requestType string, isMainnet bool) {
	if user == nil {
		return
	}
	userID := user.GetUser()
	if userID == "" {
		log.Logger.Proxy.Errorf("RequestCounter.Check (user %v): found empty userID", user.GetUser())
		return
	}

	r.mx.Lock()
	if r.counters[userID] == nil {
		r.counters[userID] = make(map[string]map[string]map[string]*auraProto.RequestsWithUsage)
	}
	if r.counters[userID][chain] == nil {
		r.counters[userID][chain] = make(map[string]map[string]*auraProto.RequestsWithUsage)
	}
	if r.counters[userID][chain][requestType] == nil {
		r.counters[userID][chain][requestType] = make(map[string]*auraProto.RequestsWithUsage)
	}
	if r.counters[userID][chain][requestType][token] == nil {
		r.counters[userID][chain][requestType][token] = &auraProto.RequestsWithUsage{
			IsMainnet: isMainnet,
		}
	}
	r.counters[userID][chain][requestType][token].Reqs++
	r.counters[userID][chain][requestType][token].Usage += creditsUsed
	r.mx.Unlock()
}

func (r *RequestCounter) flush() (err error) {
	r.mx.Lock()
	counters := r.counters
	r.counters = make(map[string]map[string]map[string]map[string]*auraProto.RequestsWithUsage)
	r.mx.Unlock()

	if len(counters) == 0 {
		return nil
	}

	timeNow := time.Now()
	protoStruct := mapCountersToProto(counters)
	for i := 0; i < 10; i++ {
		// context background used for prevent query cancellation
		_, err = r.auraAPI.IncreaseUserRequests(context.Background(), protoStruct)
		if err != nil {
			log.Logger.Proxy.Errorf("RequestCounter.flush (attempt %d): IncreaseUserRequests: %s", i, err)
			continue
		}

		log.Logger.Proxy.Debugf("RequestCounter flushed %d items. Elapsed time %s", len(counters), time.Since(timeNow))
		return nil
	}

	return err
}

func mapCountersToProto(counters map[string]map[string]map[string]map[string]*auraProto.RequestsWithUsage) *auraProto.IncreaseUserRequestsReq {
	protoReq := &auraProto.IncreaseUserRequestsReq{
		Reqs: make(map[string]*auraProto.UserRequestsByChain),
	}

	for user, chains := range counters {
		userRequestsByChain := &auraProto.UserRequestsByChain{
			Reqs: make(map[string]*auraProto.UserRequestsByRequestType),
		}
		for chain, requestType := range chains {
			userRequestsByType := &auraProto.UserRequestsByRequestType{
				Reqs: make(map[string]*auraProto.UserRequestsByToken),
			}
			for requestType, token := range requestType {
				userRequestsByToken := &auraProto.UserRequestsByToken{
					Reqs: make(map[string]*auraProto.RequestsWithUsage),
				}
				for token, count := range token {
					userRequestsByToken.Reqs[token] = count
				}
				userRequestsByType.Reqs[requestType] = userRequestsByToken
			}
			userRequestsByChain.Reqs[chain] = userRequestsByType
		}
		protoReq.Reqs[user] = userRequestsByChain
	}

	return protoReq
}
