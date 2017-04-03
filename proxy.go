package main

import (
	"time"

	"github.com/golang/glog"
)

const proxyChannelSize = 10000

type Stat struct {
	req_num  int
	ok_num   int
	fail_num int
}

type Proxy struct {
	ch    chan int
	s     Stat
	cache *Cache
}

func NewProxy(s *Server) *Proxy {
	p := &Proxy{
		ch:    make(chan int, proxyChannelSize),
		cache: NewCache(s),
	}

	// Simulate a proxy; receives a request and check quota.
	glog.Infof("Start a proxy")
	go func() {
		for true {
			_ = <-p.ch
			p.s.req_num++
			if p.cache.Check() {
				p.s.ok_num++
			} else {
				p.s.fail_num++
			}
		}
	}()

	return p
}

// Send N requests to the proxy in d duration
func (p *Proxy) Send(n int, d time.Duration) {
	s := d / time.Duration(n)

	if glog.V(2) {
		glog.Infof("Send n=%d in duration=%v: one in every %v", n, d, s)
	}

	go func() {
		for i := 0; i < n; i++ {
			p.ch <- 1
			time.Sleep(s)
		}
	}()
}
