// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package syncmap

import "sync"

// Map is just wrapper of sync.Map from Go's standard library.
// Map struct uses generics to wrap the sync.Map and gives better type safety.
// All Getters and setters associated with the map will enforce type safety for the key and value.
// Please read the documentation of sync.Map before using this Map.
type Map[K any, V any] struct {
	m sync.Map
}

func (m *Map[K, V]) Get(key K) (value V) {
	if val, ok := m.m.Load(key); ok {
		if typed, isV := val.(V); isV {
			return typed
		}
	}
	return value
}
func (m *Map[K, V]) Delete(key K) { m.m.Delete(key) }

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	val, ok := m.m.Load(key)
	if ok {
		if typed, isV := val.(V); isV {
			return typed, true
		}
	}
	return value, false
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	val, loaded := m.m.LoadAndDelete(key)
	if loaded {
		if typed, isV := val.(V); isV {
			return typed, true
		}
	}
	return value, false
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	val, loaded := m.m.LoadOrStore(key, value)
	if loaded {
		if typed, isV := val.(V); isV {
			return typed, true
		}
	}
	return value, false
}

func (m *Map[K, V]) Range(f func(key K, value V) (continueRange bool)) {
	m.m.Range(func(anyKey any, anyVal any) bool {
		key, keyOK := anyKey.(K)
		val, valOK := anyVal.(V)
		if !keyOK || !valOK {
			return true
		}
		return f(key, val)
	})
}

func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}
