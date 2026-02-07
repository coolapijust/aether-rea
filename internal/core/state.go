// Package core implements the Aether-Realist Core API.
// It provides a state-machine-based abstraction over the protocol,
// exposing only control methods and events, never protocol internals.
package core

import (
	"fmt"
	"sync"
)

// CoreState represents the finite state machine states of the Core.
type CoreState string

const (
	StateIdle     CoreState = "Idle"
	StateStarting CoreState = "Starting"
	StateActive   CoreState = "Active"
	StateRotating CoreState = "Rotating"
	StateClosing  CoreState = "Closing"
	StateClosed   CoreState = "Closed"
	StateError    CoreState = "Error"
)

// Valid transitions map: from state -> allowed to states
var validTransitions = map[CoreState][]CoreState{
	StateIdle:     {StateStarting},
	StateStarting: {StateActive, StateError},
	StateActive:   {StateRotating, StateClosing, StateError},
	StateRotating: {StateActive, StateError},
	StateClosing:  {StateClosed, StateError},
	StateClosed:   {StateStarting},
	StateError:    {StateIdle, StateClosed}, // Recovery paths
}

// StateMachine manages Core state with thread-safe transitions.
type StateMachine struct {
	mu         sync.RWMutex
	state      CoreState
	onTransition func(from, to CoreState)
}

// NewStateMachine creates a new state machine starting in Idle.
func NewStateMachine(onTransition func(from, to CoreState)) *StateMachine {
	return &StateMachine{
		state:        StateIdle,
		onTransition: onTransition,
	}
}

// State returns the current state (read-only).
func (sm *StateMachine) State() CoreState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// Transition attempts to move from current state to target state.
// Returns error if transition is not valid.
func (sm *StateMachine) Transition(to CoreState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := sm.state
	if from == to {
		return nil // No-op
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("invalid state: %s", from)
	}

	for _, s := range allowed {
		if s == to {
			sm.state = to
			if sm.onTransition != nil {
				sm.onTransition(from, to)
			}
			return nil
		}
	}

	return fmt.Errorf("invalid transition: %s -> %s", from, to)
}

// CanTransition checks if transition is valid without executing.
func (sm *StateMachine) CanTransition(to CoreState) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	from := sm.state
	if from == to {
		return true
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
