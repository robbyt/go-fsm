/*
Copyright 2024 Robert Terhaar <robbyt@robbyt.net>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fsm

import "context"

// GetStateChan returns a channel that will receive the current state of the FSM immediately.
func (fsm *Machine) GetStateChan(ctx context.Context) <-chan string {
	if ctx == nil {
		fsm.logger.Error("context is nil; cannot create state channel")
		return nil
	}

	ch := make(chan string, 1)
	unsubCallback := fsm.AddSubscriber(ch)

	go func() {
		// block here (in the background) until the context is done
		<-ctx.Done()

		// unsubscribe and close the channel
		unsubCallback()
		close(ch)
	}()

	return ch
}

// AddSubscriber adds your channel to the internal list of broadcast targets. It will receive
// the current state if the channel (if possible), and will also receive future state changes
// when the FSM state is updated. A callback function is returned that should be called to
// remove the channel from the list of subscribers when this is no longer needed.
func (fsm *Machine) AddSubscriber(ch chan string) func() {
	fsm.subscriberMutex.Lock()
	defer fsm.subscriberMutex.Unlock()

	fsm.subscribers.Store(ch, struct{}{})

	select {
	case ch <- fsm.GetState():
		fsm.logger.Debug("Sent initial state to channel")
	default:
		fsm.logger.Warn("Unable to write initial state to channel; next state change will be sent instead")
	}

	return func() {
		fsm.unsubscribe(ch)
	}
}

// unsubscribe removes a channel from the internal list of broadcast targets.
func (fsm *Machine) unsubscribe(ch chan string) {
	fsm.subscriberMutex.Lock()
	fsm.subscribers.Delete(ch)
	fsm.subscriberMutex.Unlock()
}

// broadcast sends the new state to all subscriber channels.
// If a channel is full, the state change is skipped for that channel, and a warning is logged.
// This, and the other subscriber-related methods, use a standard mutex instead of an RWMutex,
// because the broadcast sends should always be serial, and never concurrent, otherwise the order
// of state change notifications could be unpredictable.
func (fsm *Machine) broadcast(state string) {
	logger := fsm.logger.WithGroup("broadcast").With("state", state)
	fsm.subscribers.Range(func(key, value any) bool {
		ch := key.(chan string)
		select {
		case ch <- state:
			logger.Debug("Sent state to channel")
		default:
			logger.Warn("Channel is full; skipping broadcast for subscriber")
		}
		return true // continue iteration
	})
}
