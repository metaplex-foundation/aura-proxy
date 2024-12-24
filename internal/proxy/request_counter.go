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
	counters map[string]map[string]map[string]int64
	mx       sync.Mutex
}

func NewRequestCounter(ctx context.Context, wg *sync.WaitGroup, auraAPI auraProto.AuraClient) (r *RequestCounter) {
	r = &RequestCounter{
		wg:       wg,
		counters: make(map[string]map[string]map[string]int64), // providerID/chain/token/reqCount
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

func (r *RequestCounter) IncUserRequests(user *auraProto.UserWithTokens, currentReqCount int64, chain, token string) {
	if user == nil || currentReqCount == 0 {
		return
	}
	userID := user.GetUser()
	if userID == "" {
		log.Logger.Proxy.Errorf("RequestCounter.Check (user %v): found empty userID", user.GetUser())
		return
	}

	r.mx.Lock()
	if r.counters[userID] == nil {
		r.counters[userID] = make(map[string]map[string]int64)
	}
	if r.counters[userID][chain] == nil {
		r.counters[userID][chain] = make(map[string]int64)
	}
	r.counters[userID][chain][token] += currentReqCount
	r.mx.Unlock()
}

func (r *RequestCounter) flush() (err error) {
	r.mx.Lock()
	counters := r.counters
	r.counters = make(map[string]map[string]map[string]int64)
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

func mapCountersToProto(counters map[string]map[string]map[string]int64) *auraProto.IncreaseUserRequestsReq {
	protoReq := &auraProto.IncreaseUserRequestsReq{
		Reqs: make(map[string]*auraProto.UserRequestsByChain),
	}

	for user, chains := range counters {
		userRequestsByChain := &auraProto.UserRequestsByChain{
			Reqs: make(map[string]*auraProto.UserRequestsByToken),
		}
		for chain, tokens := range chains {
			userRequestsByToken := &auraProto.UserRequestsByToken{
				Reqs: make(map[string]int64),
			}
			for token, count := range tokens {
				userRequestsByToken.Reqs[token] = count
			}
			userRequestsByChain.Reqs[chain] = userRequestsByToken
		}
		protoReq.Reqs[user] = userRequestsByChain
	}

	return protoReq
}
