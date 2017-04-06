
## Introduction

A prototype code for a rate limiting prefetch algorithm.

The rate limiting feature is for a system wanted to limit request rate. For example, a proxy wants to limit request rate to protect the backend server. The exceeded requests will be rejected by the proxy and will not reach the backend server.

If a system has multiple proxy instances, rate limiting could be local or global. If local, each running instance enforces its own limit.  If global, all running instances are subjected to a global limit.  Global rate limiting is more useful than local.  For global rate limiting, usually there is a rate limiting server to enforce the global limits, each proxy instance needs to call the server to check the limits.

If each proxy instance is calling the rate limiting server for each request it is processing, it will greatly increase the request latency by adding a remote call. It is a good idea for the proxy to prefetch some tokens so not every request needs to make a remote call to check rate limits.

Here presents a prefetch algorithm for that purpose.

This prototype code presents a rate limiting prefetch algorithm. It can achieve:
* All rate limiting decisions are done at local, not need to wait for remote call.
* It works for both big rate limiting window, such as 1 minute, or small window, such as 1 second.


## Algorithm

Basic idea is:
* Use a predict window to count number of requests, use that to determine prefetch amount.
* There is a pool to store prefetch tokens from the rate limiting server.
* When the available tokens in the pool is less than half of desired amount, trigger a new prefetch.
* If a prefetch is negative (requested amount is not fully granted), need to wait for a period time before next prefetch.

There are three parameters in this algorithm:
* predictWindow: the time to count the requests, use that to determine prefetch amount
* minPrefetch: the minimum prefetch amount
* closeWaitWindow: the wait time for the next prefetch if last prefetch is negative.

The main prefetch algorithm code is in cache.go


## How to run it

```
git clone https://github.com/qiwzhang/rate-limit-prefetch
cd rate-limit-prefetch

```
You can always run this for help

```
go run *.go --help
```

Current code simulates 2 proxy instances and a rate limiting server.  Each proxy instance has its own prefetch cache.
The prefetch logic is in cache.go

For example, if you run:
```
go run *.go --alsologtostderr --rate=20 --window=1

```
It simulates a 20 requests per second rate limiting.

It will prompt:

```
Enter request number for proxy1, proxy2, duration in seconds:
120 120 10
```

If you enter 120 120 10, it means:
* send 120 requests to proxy 1 evenly in 10 seconds
* send 120 requests to proxy 2 evenly in 10 seconds

The result is showed here:

```
I0403 16:30:34.824051   27961 main.go:39] Stat: {req_num:120 ok_num:102 fail_num:18}
I0403 16:30:34.824145   27961 main.go:40] Stat: {req_num:120 ok_num:94 fail_num:26}

```
It means: 
* send 120 requests to proxy1, 102 of them granted, 18 are rejected.
* send 120 requests to proxy2, 94 of them granted, 26 are rejected.
