package main

import (
	"sync"
	"time"

	"github.com/golang/glog"
)

const (
	// Count the number of requests in this window
	// Use the amount to determine how much to prefetch
	predictWindowInSeconds = 1.0

	// How long to wait for next prefetch if last prefetch fails.
	closeWaitWindowInSeconds = 0.5

	// Minimum prefetch amount
	minPrefetchAmount = 10
)

var (
	// Converts to time.Duration type for easy use.
	predictWindow   = time.Duration(predictWindowInSeconds * float64(time.Second))
	closeWaitWindow = time.Duration(closeWaitWindowInSeconds * float64(time.Second))

	// cache_id for logging in multiple cache case
	cache_id int
)

type BucketMode int

const (
	OPEN BucketMode = iota
	CLOSE
)

// Use this always incremented ID to detect if a node has been
// re-cycled in circular list.
type NodeId uint64

type Node struct {
	available int
	id        NodeId
}

type CacheStat struct {
	prefetch_num uint64
	prefetch     uint64
	over_used    uint64
	expired      uint64
}

type Cache struct {
	queue              *Queue
	mode               BucketMode
	last_prefetch_time time.Time
	requests           *RollingWindow
	server             *Server
	s                  CacheStat
	mutex              sync.Mutex
	next_node_id       NodeId
	id                 int
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func NewCache(s *Server) *Cache {
	cache_id++
	return &Cache{
		queue:    NewQueue(10),
		requests: NewRollingWindow(predictWindow),
		server:   s,
		id:       cache_id,
	}
}

func (c *Cache) nextNodeId() NodeId {
	c.next_node_id++
	return c.next_node_id
}

// Decrease the amount from the available queue,
// return the amount that could not been decreased.
func (c *Cache) dec(delta int) int {
	n := c.queue.Head()
	for n != nil && delta > 0 {
		if n.available > 0 {
			d := min(n.available, delta)
			n.available -= d
			delta -= d
		}
		if n.available > 0 {
			return 0
		}
		c.queue.Pop()
		n = c.queue.Head()
	}
	return delta
}

func (c *Cache) countAvailable() int {
	var count int
	c.queue.Iterate(func(n *Node) bool {
		count += n.available
		return true
	})
	return count
}

func (c *Cache) tryPrefetch() {
	// If it is in Close mode, and too close to last request time,
	// Not issue a new prefetch, most likely will not get any.
	if c.mode == CLOSE &&
		time.Since(c.last_prefetch_time) < closeWaitWindow {
		return
	}

	avail := c.countAvailable()
	// Count the requests from last second window.
	pass_count := c.requests.Count()
	// Desired amount.
	amount := max(pass_count, minPrefetchAmount)
	if glog.V(4) {
		glog.Infof("[%d]tryPrefetch, avail: %d, pass: %d, req: %d",
			c.id, avail, pass_count, amount)
	}

	// If available is less than half of desired amount, prefetch them.
	if avail < amount/2 {
		// Some rare conditions to use tokens before it is granted.
		// Specifically designed not to reject the first request after
		// long period of no traffic while the prefetch is still on the way.
		use_not_granted := avail == 0 && c.mode == OPEN
		c.prefetch(amount, use_not_granted)
	}
}

// The main entry call to check the rate limit.
// It is a sync call and will return true/false right away.
func (c *Cache) Check() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if glog.V(4) {
		glog.Infof("[%d]Check called", c.id)
	}

	c.tryPrefetch()
	c.requests.Inc()
	ret := c.dec(1) == 0

	if !ret {
		if glog.V(4) {
			glog.Infof("[%d]***rejected", c.id)
		}
	}
	return ret

}

func (c *Cache) findNodeById(id NodeId) *Node {
	var found *Node
	c.queue.Iterate(func(n *Node) bool {
		if n.id == id {
			found = n
			return false
		}
		return true
	})
	return found
}

func (c *Cache) prefetch(req_amount int, use_not_granted bool) {
	var node_id NodeId
	// add the prefetch amount to available queue before it is granted.
	if use_not_granted {
		node_id = c.nextNodeId()
		n := &Node{
			available: req_amount,
			id:        node_id,
		}
		c.queue.Push(n)
	}

	if glog.V(2) {
		glog.Infof("[%d]Prefetch(%d) request amount: %v", c.id, node_id, req_amount)
	}
	c.s.prefetch_num++
	c.s.prefetch += uint64(req_amount)

	c.last_prefetch_time = time.Now()
	c.server.Alloc(req_amount, func(resp Response) {
		c.onAllocResponse(node_id, req_amount, resp)
	})
}

func (c *Cache) onAllocResponse(node_id NodeId, req_amount int, resp Response) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var n *Node
	// The prefetched amount was added to the available queue
	if node_id != 0 {
		n = c.findNodeById(node_id)
		if resp.amount < req_amount {
			delta := req_amount - resp.amount
			// Substract it from its own request node.
			if n != nil {
				d := min(n.available, delta)
				n.available -= d
				delta -= d
			}
			if delta > 0 {
				// Substract it from other prefetched amounts
				d := c.dec(delta)
				if d > 0 {
					// These are over-used amount
					c.s.over_used += uint64(d)
					glog.Infof("[%d]===Over-used %v", c.id, d)
				}
			}
		}
	} else {
		if resp.amount > 0 {
			node_id = c.nextNodeId()
			n = &Node{
				available: resp.amount,
				id:        node_id,
			}
			c.queue.Push(n)
		}
	}

	if glog.V(2) {
		glog.Infof("[%d]Prefetch(%d) granted: %v", c.id, node_id, resp.amount)
	}
	if resp.amount == req_amount {
		if c.mode != OPEN {
			c.mode = OPEN
			if glog.V(2) {
				glog.Infof("[%d]Prefetch(%d) change mode to OPEN", c.id, node_id)
			}
		}
	} else {
		if c.mode != CLOSE {
			c.mode = CLOSE
			if glog.V(2) {
				glog.Infof("[%d]Prefetch(%d) change mode to CLOSE", c.id, node_id)
			}
		}
	}

	if n != nil && n.available > 0 {
		if glog.V(4) {
			glog.Infof("[%d]add timer to expire id=%d, in: %v second", c.id, node_id, resp.expire)
		}
		time.AfterFunc(resp.expire*time.Second, func() {
			c.onExpire(node_id)
		})
	}
}

func (c *Cache) onExpire(node_id NodeId) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Called when prefetched tokens are expired.
	n := c.findNodeById(node_id)
	if n == nil {
		return
	}

	if n.available > 0 {
		c.s.expired += uint64(n.available)
		if glog.V(2) {
			glog.Infof("[%d]===Expired %v from id: %d", c.id, n.available, node_id)
		}
		n.available = 0
	}
}
