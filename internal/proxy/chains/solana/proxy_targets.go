package solana

import (
	"sync"
	"time"

	"aura-proxy/internal/pkg/chains/solana"
	"aura-proxy/internal/pkg/log"
	"aura-proxy/internal/pkg/models"
	"aura-proxy/internal/pkg/util/echo"
)

type (
	ProxyTarget struct {
		availableMethods map[string]targetRestriction
		provider         string
		targetType       solana.NodeType
		url              string
		reqCounter       uint64
		reqLimit         uint64
		reqWindow        int64
		slotAmount       int64

		mx sync.RWMutex
	}

	targetRestriction struct {
		lastResponsesTimeMs []int64 // store last 10 value
		jailExpireTime      int64
		errCounter          uint64
		successCounter      uint64
	}
)

const (
	lastResponsesTimeMsArrLen = 10
	noFullHistoryPenalty      = 1
)

func NewProxyTarget(urlWithMethods models.URLWithMethods, reqLimit uint64, provider string, targetType solana.NodeType) *ProxyTarget {
	pt := ProxyTarget{
		url:              urlWithMethods.URL,
		reqLimit:         reqLimit,
		provider:         provider,
		targetType:       targetType,
		availableMethods: make(map[string]targetRestriction, len(urlWithMethods.SupportedMethods)),
		slotAmount:       urlWithMethods.SlotAmount,
	}
	for _, sm := range urlWithMethods.SupportedMethods {
		pt.availableMethods[sm.Name] = targetRestriction{
			lastResponsesTimeMs: []int64{sm.ResponseTimeMs},
		}
	}

	return &pt
}

func (t *ProxyTarget) isAvailable(reqMethods []string, reqType models.TokenType, mainnetSlot int64, getSlotTime time.Time, c *echo.CustomContext) (isAvailable bool, failedReqs uint64, lastRespTime int64) {
	currentWindow, timeNow := getCurrentTimeWindow()

	t.mx.RLock()
	defer t.mx.RUnlock()
	// check req limit
	if t.reqLimit > 0 && currentWindow == t.reqWindow && t.reqCounter >= t.reqLimit {
		return false, failedReqs, lastRespTime
	}

	for _, rm := range reqMethods {
		iSupportedMethod, err := t.targetType.IsSupportMethod(rm)
		if err != nil {

		}
		if !iSupportedMethod {
			return false, failedReqs, lastRespTime
		}
		if solana.BlockRelatedMethod(rm) {
			notContainBlock := t.targetType.Name != solana.ArchiveSolanaNode && c.GetReqBlock() < calculateSlot(mainnetSlot, getSlotTime, t.targetType.AvailableSlotsHistory)
			if notContainBlock {
				return false, failedReqs, lastRespTime
			}
		}
		am, _ := t.availableMethods[rm]
		if am.jailExpireTime > timeNow {
			return false, failedReqs, lastRespTime
		}
		if reqType == models.ReliableTokenType && am.errCounter > failedReqs { // return higher errCounter for current target
			failedReqs = am.errCounter
			if t.targetType.Name != solana.ArchiveSolanaNode && solana.TxRelatedMethod(rm) {
				failedReqs = failedReqs*2 + noFullHistoryPenalty
			}
		}
		if reqType == models.SpeedTokenType { // return higher lastResponseTimeMs for current target
			if methodRespTime := am.getLastResponsesTimeMs(); methodRespTime > lastRespTime {
				lastRespTime = methodRespTime
			}
		}
	}

	return true, failedReqs, lastRespTime
}
func (t *ProxyTarget) UpdateStats(success bool, reqMethods []string, responseTimeMs, slotAmount int64) {
	currentWindow, _ := getCurrentTimeWindow()

	t.mx.Lock()

	// truncate req counter by window
	if currentWindow > t.reqWindow {
		t.reqWindow = currentWindow
		t.reqCounter = 0
	}

	// increment req counter
	t.reqCounter++
	if slotAmount != 0 {
		t.slotAmount = slotAmount
	}

	for _, rm := range reqMethods {
		// get inner struct
		restriction, ok := t.availableMethods[rm]
		if ok {
			if success { // apply only success req time
				restriction.addLastResponsesTimeMs(responseTimeMs)
			}
		} else {
			if !success {
				log.Logger.Proxy.Debugf("UpdateStats: banned %s %s", t.url, rm) // TODO: temp log
			} else {
				restriction.lastResponsesTimeMs = []int64{responseTimeMs}                    // init new
				log.Logger.Proxy.Debugf("UpdateStats: successfully tested %s %s", t.url, rm) // TODO: temp log
			}
		}

		switch {
		case !success:
			restriction.successCounter = 0
			restriction.errCounter++
			restriction.jailExpireTime = time.Now().Add(targetJailTime * time.Duration(restriction.errCounter)).Unix()
			log.Logger.Proxy.Debugf("UpdateStats: target jailed %s %s for %s", t.url, rm, targetJailTime*time.Duration(restriction.errCounter)) // TODO: temp log
		case restriction.successCounter < consecutiveSuccessResponses:
			restriction.successCounter++
		default:
			restriction.successCounter = 0
			restriction.errCounter = 0
		}

		// save back
		t.availableMethods[rm] = restriction
	}
	t.mx.Unlock()
}

func getCurrentTimeWindow() (int64, int64) { //nolint:gocritic,revive
	timeNow := time.Now()
	return timeNow.Truncate(time.Second * limitWindowSeconds).Unix(), timeNow.Unix()
}

func (t *targetRestriction) addLastResponsesTimeMs(v int64) {
	t.lastResponsesTimeMs = append(t.lastResponsesTimeMs, v)
	if len(t.lastResponsesTimeMs) > lastResponsesTimeMsArrLen {
		t.lastResponsesTimeMs = t.lastResponsesTimeMs[len(t.lastResponsesTimeMs)-lastResponsesTimeMsArrLen:]
	}
}
func (t *targetRestriction) getLastResponsesTimeMs() (res int64) {
	if len(t.lastResponsesTimeMs) == 0 {
		return
	}

	for _, rt := range t.lastResponsesTimeMs {
		res += rt
	}

	return res / int64(len(t.lastResponsesTimeMs))
}
