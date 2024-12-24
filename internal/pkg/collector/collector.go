package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	auraProto "github.com/adm-metaex/aura-api/pkg/proto"

	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/util"
)

const (
	flushAmount = 1000
)

type (
	collectorPossibleTypes interface {
		*auraProto.Stat | *auraProto.DetailedRequest
	}
	Collector[T collectorPossibleTypes] struct {
		auraAPI       auraProto.AuraClient
		cache         []T
		mx            sync.Mutex
		flushInterval time.Duration
	}
)

func NewCollector[T collectorPossibleTypes](ctx context.Context, flushInterval time.Duration, auraAPI auraProto.AuraClient) (c *Collector[T], err error) {
	if auraAPI == nil {
		return nil, errors.New("empty auraAPI")
	}

	c = &Collector[T]{
		flushInterval: flushInterval,
		cache:         make([]T, 0, flushAmount),
		auraAPI:       auraAPI,
	}

	err = util.AsyncRunWithInterval(ctx, nil, flushInterval, false, true, func(ctx context.Context) error {
		err := c.flushData(ctx)
		if err != nil {
			log.Logger.Collector.Errorf("Collector: flushData: %s", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return
}

func (c *Collector[T]) flushData(ctx context.Context) error {
	entries := c.getCachedEntries()
	if len(entries) == 0 {
		return nil
	}

	var (
		err     error
		caller  string
		timeNow = time.Now()
	)
	switch e := any(entries).(type) {
	case []*auraProto.Stat:
		caller = "BatchInsertStats"
		_, err = c.auraAPI.BatchInsertStats(ctx, &auraProto.BatchInsertStatsReq{Stats: e})
	case []*auraProto.DetailedRequest:
		caller = "BatchInsertDetailedRequests"
		_, err = c.auraAPI.BatchInsertDetailedRequests(ctx, &auraProto.BatchInsertDetailedRequestsReq{Req: e})
	default:
		return fmt.Errorf("unknow type to handle: %T", e)
	}
	if err != nil {
		return fmt.Errorf("%s: %s", caller, err)
	}

	log.Logger.Collector.Debugf("fin flushData %s len %d. Elapsed %s", caller, len(entries), time.Since(timeNow))

	return nil
}

func (c *Collector[collectorPossibleTypes]) Add(s collectorPossibleTypes) {
	c.mx.Lock()
	c.cache = append(c.cache, s)
	c.mx.Unlock()
}
func (c *Collector[collectorPossibleTypes]) getCachedEntries() (s []collectorPossibleTypes) {
	c.mx.Lock()
	s = c.cache
	c.cache = make([]collectorPossibleTypes, 0, flushAmount)
	c.mx.Unlock()

	return
}
