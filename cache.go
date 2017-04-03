package main

import (
	"sync"
	"time"

	"github.com/golang/glog"
)

const (
	// The desired prefetch window in seconds
	// The algorithm will dynamically adjust prefetch tokens
	// so it can last the window time.
	// For example, if it is 2,  the prefetched tokens will last 2 seconds.
	// In another word, prefetch is only called once in every
	// prefetchWindow.
	prefetchWindowInSeconds = 1
)

var (
	// Converts to time.Duration type for easy use.
	prefetchWindow = time.Duration(prefetchWindowInSeconds) * time.Second

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
	prefetch_amount    int
	mode               BucketMode
	last_prefetch_time time.Time
	queue              *Queue
	server             *Server
	s                  CacheStat
	mutex              sync.Mutex
	next_node_id       NodeId
	id                 int
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
		prefetch_amount: 1, // starts with prefetch amount 1
		mode:            OPEN,
		queue:           NewQueue(10),
		server:          s,
		next_node_id:    1, // node id starts with 2
		id:              cache_id,
	}
}

// Special node ids:
// id=0: prefetch amount is not added to available queue in CLOSE mode.
// id=1: prefetch amount is 1 and used in OPEN mode.
// Other ids starts with 2.
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

// The main entry call to check the rate limit.
// It is a sync call and will return true/false right away.
func (c *Cache) Check() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// If available queue can successfully substract 1, grant the request.
	if c.dec(1) == 0 {
		return true
	}

	if c.last_prefetch_time.IsZero() {
		return c.prefetch()
	}

	d := time.Since(c.last_prefetch_time)
	if d > prefetchWindow {
		// If prefetched token lasts too long, adjust it down.
		if d > 2*prefetchWindow {
			c.prefetch_amount /= int(d / 2 * prefetchWindow)
			if c.prefetch_amount < 1 {
				c.prefetch_amount = 1
			}
			if glog.V(2) {
				glog.Infof("[%d]Prefetch amount decreased to: %v", c.id, c.prefetch_amount)
			}
		}
		return c.prefetch()
	}

	if c.mode == OPEN {
		// prefetch amount is too small, increase it.
		c.prefetch_amount *= 2
		if glog.V(2) {
			glog.Infof("[%d]Prefetch amount increased to: %v", c.id, c.prefetch_amount)
		}
		return c.prefetch()
	}
	return false
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

func (c *Cache) prefetch() bool {
	var node_id NodeId
	req_amount := c.prefetch_amount
	if c.mode == OPEN {
		if req_amount > 1 {
			// Add the prefetch amount to available queue even before it is
			// responded. Minus 1 to account for this request.
			node_id = c.nextNodeId()
			n := &Node{
				available: req_amount - 1,
				id:        node_id,
			}
			c.queue.Push(n)
		} else {
			// Use special node id=1 to indicate it is OPEN and req_amount=1
			node_id = 1
		}
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

	return c.mode == OPEN
}

func (c *Cache) onAllocResponse(node_id NodeId, req_amount int, resp Response) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if glog.V(2) {
		glog.Infof("[%d]Prefetch(%d) granted: %v", c.id, node_id, resp.amount)
	}
	var n *Node
	if node_id > 1 {
		// The prefetched amount was added to the available queue
		n = c.findNodeById(node_id)
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
		delta := req_amount - resp.amount

		// Be conservative, decrease prefetch amount.
		c.prefetch_amount -= delta
		if c.prefetch_amount < 1 {
			c.prefetch_amount = 1
		}
		if glog.V(2) {
			glog.Infof("[%d]Prefetch amount decreased to: %v", c.id, c.prefetch_amount)
		}

		// The granted amount is less than requested and it has been
		// added to the available queue at the request time,
		// substract the delta from the available queue.
		if node_id > 1 {
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
	}

	// node_id=0 means prefetched amount was not added to the available queue at
	// request time for CLOSE mode. Add it now.
	if node_id == 0 && resp.amount > 0 {
		node_id = c.nextNodeId()
		n = &Node{
			available: resp.amount,
			id:        node_id,
		}
		c.queue.Push(n)
	}

	// If granted tokens are not all used, set expiration timer.
	if n != nil && n.available > 0 {
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
	if n == nil || n.available <= 0 {
		return
	}

	// prefetch is too big, adjust it
	// to actual usage.
	c.prefetch_amount -= n.available
	if c.prefetch_amount < 1 {
		c.prefetch_amount = 1
	}
	if glog.V(2) {
		glog.Infof("[%d]Prefetch amount decreased to: %v", c.id, c.prefetch_amount)
	}

	c.s.expired += uint64(n.available)
	glog.Infof("[%d]===Expired %v", c.id, n.available)

	// Wipe out the tokens
	n.available = 0
	// If there are token avaiable,
	// it should be OPEN
	c.mode = OPEN
}
