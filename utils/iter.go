package utils

import "iter"

func IterMap[K comparable, V any](m map[K]V) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for key, value := range m {
			if !yield(key, value) {
				return
			}
		}
	}
}

func Join2[K any, V any](a iter.Seq2[K, V], b iter.Seq2[K, V]) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range a {
			if !yield(k, v) {
				return
			}
		}
		for k, v := range b {
			if !yield(k, v) {
				return
			}
		}
	}
}
