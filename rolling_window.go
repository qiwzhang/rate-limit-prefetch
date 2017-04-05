package main

import (
	"time"
)

const (
	windowSize = 10
)

type RollingWindow struct {
	slots []int
	count int
	tail  int
	last  time.Time
	d     time.Duration
}

func NewRollingWindow(d time.Duration) *RollingWindow {
	return &RollingWindow{
		slots: make([]int, windowSize),
		last:  time.Now(),
		d:     d / windowSize,
	}
}

func (r *RollingWindow) clear() {
	r.last = time.Now()
	for i := 0; i < windowSize; i++ {
		r.slots[i] = 0
	}
	r.tail = 0
	r.count = 0
}

func (r *RollingWindow) roll() {
	d := time.Since(r.last)
	n := int(d / r.d)

	// Completely wipe out.
	if n >= windowSize {
		r.clear()
		return
	}

	for i := 0; i < n; i++ {
		r.tail = (r.tail + 1) % len(r.slots)
		r.count -= r.slots[r.tail]
		r.slots[r.tail] = 0
		r.last = r.last.Add(r.d)
	}
}

func (r *RollingWindow) Inc() {
	r.roll()
	r.slots[r.tail]++
	r.count++
}

func (r *RollingWindow) Count() int {
	r.roll()
	return r.count
}
