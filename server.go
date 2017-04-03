package main

import (
	"time"

	"github.com/golang/glog"
)

const serverChannelSize = 10000

type Response struct {
	amount int
	expire time.Duration
}

type ResponseFunc func(Response)

type Request struct {
	amount int
	fn     ResponseFunc
}

type DelayData struct {
	resp Response
	fn   ResponseFunc
	t    time.Time
}

type Delay struct {
	ch chan DelayData
}

func newDelay() *Delay {
	ch := make(chan DelayData, serverChannelSize)
	go func() {
		latency := time.Duration(*flag_latency)
		glog.Infof("Introduce latency: %v\n", latency)
		for true {
			r := <-ch
			d := time.Since(r.t)
			if d < latency {
				time.Sleep(time.Duration(latency-d) *
					time.Millisecond)
			}
			// Run the response function
			go r.fn(r.resp)
		}
	}()
	return &Delay{ch: ch}
}

func (d *Delay) delay(fn ResponseFunc, resp Response) {
	d.ch <- DelayData{
		resp: resp,
		fn:   fn,
		t:    time.Now(),
	}
}

func (d *Delay) empty() bool {
	return len(d.ch) == 0
}

type Server struct {
	ch chan Request
	d  *Delay
}

func NewServer() *Server {
	s := &Server{
		ch: make(chan Request, serverChannelSize),
		d:  newDelay(),
	}
	go func() {
		rate := float64(*flag_rate)
		window := float64(*flag_window)
		glog.Infof("rate limit: rate=%f, window=%f\n", rate, window)

		allowance := rate
		last_check := time.Now()
		for true {
			req := <-s.ch
			curr := time.Now()
			duration := curr.Sub(last_check)
			last_check = curr
			allowance += duration.Seconds() * (rate / window)
			if allowance > rate {
				allowance = rate // throttle
			}
			granted := allowance
			if float64(req.amount) < granted {
				granted = float64(req.amount)
			}
			allowance -= granted

			s.d.delay(req.fn, Response{
				amount: int(granted),
				expire: time.Duration(window),
			})
		}
	}()
	return s
}

func (s *Server) Alloc(amount int, fn ResponseFunc) {
	s.ch <- Request{
		amount: amount,
		fn:     fn,
	}
}

func (s *Server) empty() bool {
	return len(s.ch) == 0 && s.d.empty()
}
