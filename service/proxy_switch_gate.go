package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
)

var (
	ErrProxySwitchQueueFull = errors.New("proxy group switch waiting queue is full")
	ErrProxySwitchTimeout   = errors.New("timed out waiting for proxy group switch")
)

type proxyGroupSwitchGate struct {
	mu              sync.Mutex
	switching       bool
	activeSelectors int
	waitingRequests int
	changed         chan struct{}
}

var proxyGroupSwitchGates sync.Map // map[int]*proxyGroupSwitchGate

func getProxyGroupSwitchGate(groupId int) *proxyGroupSwitchGate {
	value, _ := proxyGroupSwitchGates.LoadOrStore(groupId, &proxyGroupSwitchGate{changed: make(chan struct{})})
	return value.(*proxyGroupSwitchGate)
}

func (gate *proxyGroupSwitchGate) notifyLocked() {
	close(gate.changed)
	gate.changed = make(chan struct{})
}

// acquireSelection prevents a switch from starting while a request is choosing
// its proxy. Already selected/sent requests are intentionally not held.
func (gate *proxyGroupSwitchGate) acquireSelection(ctx context.Context, maxWaiting int) (func(), error) {
	countedWaiting := false
	for {
		gate.mu.Lock()
		if !gate.switching {
			if countedWaiting {
				gate.waitingRequests--
			}
			gate.activeSelectors++
			gate.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					gate.mu.Lock()
					gate.activeSelectors--
					gate.notifyLocked()
					gate.mu.Unlock()
				})
			}, nil
		}
		if !countedWaiting {
			if maxWaiting > 0 && gate.waitingRequests >= maxWaiting {
				gate.mu.Unlock()
				return nil, ErrProxySwitchQueueFull
			}
			gate.waitingRequests++
			countedWaiting = true
		}
		changed := gate.changed
		gate.mu.Unlock()

		select {
		case <-ctx.Done():
			gate.mu.Lock()
			if countedWaiting {
				gate.waitingRequests--
			}
			gate.mu.Unlock()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, ErrProxySwitchTimeout
			}
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (gate *proxyGroupSwitchGate) beginSwitch(ctx context.Context) (bool, error) {
	gate.mu.Lock()
	if gate.switching {
		gate.mu.Unlock()
		return false, nil
	}
	gate.switching = true
	gate.notifyLocked()
	for gate.activeSelectors > 0 {
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-ctx.Done():
			gate.mu.Lock()
			gate.switching = false
			gate.notifyLocked()
			gate.mu.Unlock()
			return false, ctx.Err()
		case <-changed:
		}
		gate.mu.Lock()
	}
	gate.mu.Unlock()
	return true, nil
}

func (gate *proxyGroupSwitchGate) finishSwitch() {
	gate.mu.Lock()
	gate.switching = false
	gate.notifyLocked()
	gate.mu.Unlock()
}

func (gate *proxyGroupSwitchGate) waitForAvailability(ctx context.Context, maxWaiting int, ready func() (bool, error)) error {
	gate.mu.Lock()
	if maxWaiting > 0 && gate.waitingRequests >= maxWaiting {
		gate.mu.Unlock()
		return ErrProxySwitchQueueFull
	}
	gate.waitingRequests++
	gate.mu.Unlock()
	defer func() {
		gate.mu.Lock()
		gate.waitingRequests--
		gate.mu.Unlock()
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		isReady, err := ready()
		if err != nil {
			return err
		}
		if isReady {
			return nil
		}
		gate.mu.Lock()
		changed := gate.changed
		gate.mu.Unlock()
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ErrProxySwitchTimeout
			}
			return ctx.Err()
		case <-changed:
		case <-ticker.C:
		}
	}
}

func proxySwitchWaitContext(ctx context.Context, waitSeconds int) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if waitSeconds <= 0 {
		waitSeconds = model.DefaultProxySwitchWaitSeconds
	}
	return context.WithTimeout(ctx, time.Duration(waitSeconds)*time.Second)
}

func switchManagedProxyGroup(ctx context.Context, groupId int, failedProxyId int, waitSeconds int) (int, error) {
	waitCtx, cancel := proxySwitchWaitContext(ctx, waitSeconds)
	defer cancel()
	gate := getProxyGroupSwitchGate(groupId)
	owner, err := gate.beginSwitch(waitCtx)
	if err != nil {
		_ = model.AbortProxyGroupSwitch(groupId)
		return 0, err
	}
	if !owner {
		return 0, nil
	}
	defer gate.finishSwitch()

	nextProxyId, err := model.CompleteProxyGroupSwitch(groupId, failedProxyId)
	if err != nil {
		_ = model.AbortProxyGroupSwitch(groupId)
		return 0, err
	}
	InvalidateChannelProxyConfig(0)
	return nextProxyId, nil
}

func switchManagedProxyGroupTo(ctx context.Context, groupId int, targetProxyId int, waitSeconds int) error {
	waitCtx, cancel := proxySwitchWaitContext(ctx, waitSeconds)
	defer cancel()
	gate := getProxyGroupSwitchGate(groupId)
	owner, err := gate.beginSwitch(waitCtx)
	if err != nil {
		return err
	}
	if !owner {
		return nil
	}
	defer gate.finishSwitch()
	if err := model.SetProxyGroupRecoveryProbe(groupId, targetProxyId); err != nil {
		return err
	}
	InvalidateChannelProxyConfig(0)
	return nil
}

func SetManagedProxyPaused(ctx context.Context, proxyId int, paused bool) error {
	result, err := model.SetProxyManualPaused(proxyId, paused)
	if err != nil {
		return err
	}
	InvalidateChannelProxyConfig(0)
	if result.SwitchRequired {
		_, err = switchManagedProxyGroup(ctx, result.ProxyGroupId, result.ProxyId, result.SwitchWaitSeconds)
		return err
	}
	if !paused {
		_, err = CheckManagedProxyNow(ctx, proxyId)
	}
	return err
}

func SwitchManagedProxyGroupNow(ctx context.Context, groupId int) (int, error) {
	currentProxyId, waitSeconds, err := model.PrepareManualProxyGroupSwitch(groupId)
	if err != nil {
		return 0, err
	}
	InvalidateChannelProxyConfig(0)
	return switchManagedProxyGroup(ctx, groupId, currentProxyId, waitSeconds)
}

func resetProxyGroupSwitchGatesForTest() {
	proxyGroupSwitchGates = sync.Map{}
}
