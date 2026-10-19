//go:build race

package engine

// raceEnabled is true when tests run under the race detector, which slows Compute by 5–20×.
const raceEnabled = true
