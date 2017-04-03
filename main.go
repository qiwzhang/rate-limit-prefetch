package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/golang/glog"
)

var (
	flag_rate    = flag.Int("rate", 100, "the rate limit in a window")
	flag_window  = flag.Int("window", 60, "rate limiting widnow in seconds")
	flag_latency = flag.Int("latency", 200, "latency in ms for rate limit calls")
)

func main() {
	flag.Parse()
	s := NewServer()

	// Create two proxy
	p1 := NewProxy(s)
	p2 := NewProxy(s)

	for true {
		fmt.Println("Enter request number for proxy1, proxy2, duration in seconds:")
		var n1, n2, d int
		fmt.Scanln(&n1, &n2, &d)

		dur := time.Duration(d) * time.Second
		p1.Send(n1, dur)
		p2.Send(n2, dur)
		time.Sleep(dur)

		glog.Infof("Done sending, wait for results...")
		for !s.empty() {
			time.Sleep(time.Millisecond)
		}
		glog.Infof("Stat: %+v\n", p1.s)
		glog.Infof("Stat: %+v\n", p2.s)
	}
}
