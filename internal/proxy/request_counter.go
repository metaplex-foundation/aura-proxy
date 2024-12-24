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
	counters map[string]int64
	mx       sync.Mutex
}

func NewRequestCounter(ctx context.Context, wg *sync.WaitGroup, auraAPI auraProto.AuraClient) (r *RequestCounter) {
	r = &RequestCounter{
		wg:       wg,
		counters: make(map[string]int64), // providerID/reqCount
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

func (r *RequestCounter) IncUserRequests(user *auraProto.UserWithTokens, currentReqCount int64) {
	if user == nil || currentReqCount == 0 {
		return
	}
	userID := user.GetUser()
	if userID == "" {
		log.Logger.Proxy.Errorf("RequestCounter.Check (user %v): found empty userID", user.GetUser())
		return
	}

	r.mx.Lock()
	r.counters[userID] += currentReqCount
	r.mx.Unlock()
}

func (r *RequestCounter) flush() (err error) {
	r.mx.Lock()
	counters := r.counters
	r.counters = make(map[string]int64)
	r.mx.Unlock()

	if len(counters) == 0 {
		return nil
	}

	timeNow := time.Now()
	for i := 0; i < 10; i++ {
		// context background used for prevent query cancellation
		_, err = r.auraAPI.IncreaseUserRequests(context.Background(), &auraProto.IncreaseUserRequestsReq{Reqs: counters})
		if err != nil {
			log.Logger.Proxy.Errorf("RequestCounter.flush (attempt %d): IncreaseUserRequests: %s", i, err)
			continue
		}

		log.Logger.Proxy.Debugf("RequestCounter flushed %d items. Elapsed time %s", len(counters), time.Since(timeNow))
		return nil
	}

	return err
}
