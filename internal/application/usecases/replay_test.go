package usecases_test

import "github.com/IQVO/payment-provider-simulator/internal/application/views"

// replayOf is the answer a replay of first must return: the stored answer,
// marked Replayed (the first execution never is).
func replayOf(first views.Answer) views.Answer {
	if first.Replayed {
		panic("a first execution is never marked Replayed")
	}
	first.Replayed = true
	return first
}
