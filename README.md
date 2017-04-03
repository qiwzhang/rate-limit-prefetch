This is a prototype code for a rate limiting prefetch algorithm.

The rate limiting feature is for a system wanted to limit request rate. For example, a proxy wants to only allow request rate less than certain amount reaching the backend server. The exceeded requests will be rejected by the proxy and will not reach the backend server.
If there are multiple proxy instances, rate limiting could be local or global. If local, each running instance has its own limit.  If global, all running instances are subjected to the global limit.  Global rate limiting is more useful than local.  For global rate limiting, usually there is a rate limiting server to enforce the global limit, each proxy instance needs to call the server.
If each proxy instance is calling the rate limiting server for each request, it will greatly increase the request latency by adding a remote call. Each proxy (the rate limiting client) is recommended to prefetch some tokens in order to reduce the latency.
This prototype code presents a rate limiting prefetch algorithm. It can achieve:
* All rate limiting decisions are done at local, not need to wait for remote call.
* It works for both big rate limiting window, such as 1 minutes, or small window, such as 1 second.

## How to run it

git clone https://github.com/qiwzhang/rate-limit-prefetch
cd rate-limit-prefetch

You can always run for help

```
go run *.go --help
```

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
