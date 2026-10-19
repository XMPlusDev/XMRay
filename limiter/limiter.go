package limiter

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"golang.org/x/time/rate"

	"github.com/xmplusdev/xmray/api"
)

type SubscriptionInfo struct {
	Id         int
	SpeedLimit uint64
	IPLimit    int
}

// ipKeyPrefix namespaces the per-subscription IP hashes.
//
// Shared verbatim with XMBox. ip_limit is a property of the subscription, not
// of whichever backend served the connection, so both must count into the same
// hash — otherwise a user reaches the limit separately on each and effectively
// gets twice the addresses. This relies on node tags being unique across both,
// which holds because each tag carries the panel's node ID.
//
// Also deliberately different from the key the previous format used: that one
// held a serialised map as a plain string, and a hash command against it would
// fail with WRONGTYPE. Old keys expire on their own TTL.
const ipKeyPrefix = "xmplus:ip:"

// ipKey returns the hash holding one subscription's connected addresses.
func ipKey(subscription string) string { return ipKeyPrefix + subscription }

// redisBatchSize is how many commands go into one pipelined round trip.
//
// The online-IP sweep touches every subscription on the node. Issued one at a
// time that is thousands of sequential round trips, which is what exhausted the
// deadline; pipelined, it is a handful.
const redisBatchSize = 256

// ipField identifies one address on one node. The tag is part of the field so a
// single address in use on two nodes stays distinguishable; "|" is a safe
// separator because neither IPv4 nor IPv6 literals contain it.
func ipField(ip, tag string) string { return ip + "|" + tag }

// ipLimitScript decides whether a connection is allowed and records it, in one
// atomic step.
//
// This replaces a read-modify-write of the whole address map: two connections
// for the same subscription would each read the map, add their own address and
// write the result back, so whichever finished last erased the other's entry.
// Running the check and the insert together inside Redis removes that window,
// and touching a single field rather than rewriting the map means concurrent
// connections no longer overwrite each other at all.
//
// KEYS[1] the subscription's hash. ARGV: 1 field, 2 UID, 3 IP limit (0 means
// unlimited), 4 TTL in seconds, 5 the address on its own.
// Returns 1 when the connection must be rejected, 0 when it is allowed.
var ipLimitScript = redis.NewScript(`
local field = ARGV[1]
if redis.call('HEXISTS', KEYS[1], field) == 0 then
  local limit = tonumber(ARGV[3])
  if limit > 0 then
    local seen = {}
    local distinct = 0
    for _, f in ipairs(redis.call('HKEYS', KEYS[1])) do
      local addr = string.match(f, '^(.+)|[^|]*$')
      if addr and not seen[addr] then
        seen[addr] = true
        distinct = distinct + 1
      end
    end
    -- An address already counted under another node must not be turned away:
    -- the limit is on distinct addresses, not on connections.
    if distinct >= limit and not seen[ARGV[5]] then
      return 1
    end
  end
end
redis.call('HSET', KEYS[1], field, ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 0
`)

type InboundInfo struct {
	Tag              string
	NodeSpeedLimit   uint64
	IgnoreIPs        []string
	SubscriptionInfo *sync.Map // key: email → SubscriptionInfo
	BucketHub        *sync.Map // key: email → *rate.Limiter
	GlobalIPLimit    struct {
		config *RedisConfig
		client *redis.Client
		expiry time.Duration
	}
}

type Limiter struct {
	server      *core.Instance
	InboundInfo *sync.Map
	stm         stats.Manager
	redisConfig *RedisConfig
	redisClient *redis.Client
}

func New(server *core.Instance, redisConfig *RedisConfig) *Limiter {
	l := &Limiter{
		server:      server,
		InboundInfo: new(sync.Map),
		stm:         server.GetFeature(stats.ManagerType()).(stats.Manager),
		redisConfig: redisConfig,
	}
	if redisConfig != nil && redisConfig.Enable {
		timeout := redisConfig.timeout()
		l.redisClient = redis.NewClient(&redis.Options{
			Network:  redisConfig.Network,
			Addr:     redisConfig.Addr,
			Username: redisConfig.Username,
			Password: redisConfig.Password,
			DB:       redisConfig.DB,
			// The pool was fixed at 10 regardless of how many connections this
			// node accepts, so commands queued for a slot and that wait counted
			// against their deadline.
			PoolSize:    redisConfig.poolSize(),
			DialTimeout: timeout,
			// Fail a command that cannot get a pool slot in time instead of
			// letting it sit there until the caller's context expires.
			PoolTimeout:  timeout,
			ReadTimeout:  timeout,
			WriteTimeout: timeout,
		})
		log.Printf("[Limiter] Redis initialised at %s", redisConfig.Addr)
	}
	return l
}

// Close shuts down the shared Redis client. Call this when the instance stops.
func (l *Limiter) Close() {
	if l.redisClient != nil {
		if err := l.redisClient.Close(); err != nil {
			log.Printf("[Limiter] error closing Redis client: %v", err)
		}
	}
}

func (l *Limiter) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, ignoreIPs []string, subscriptionList *[]api.SubscriptionInfo) error {
	inboundInfo := &InboundInfo{
		Tag:            tag,
		NodeSpeedLimit: nodeSpeedLimit,
		IgnoreIPs:      ignoreIPs,
		BucketHub:      new(sync.Map),
	}

	if l.redisClient != nil {
		// Nodes share one Redis connection; the TTL is per node.
		inboundInfo.GlobalIPLimit.config = l.redisConfig
		inboundInfo.GlobalIPLimit.client = l.redisClient
		inboundInfo.GlobalIPLimit.expiry = time.Duration(expiry) * time.Second
	} else {
		log.Printf("[Limiter] Redis disabled for tag %s — IP limiting unavailable", tag)
	}

	subscriptionMap := new(sync.Map)
	for _, u := range *subscriptionList {
		key := fmt.Sprintf("%s_%s", tag, u.Email)
		subscriptionMap.Store(key, SubscriptionInfo{
			Id:         u.Id,
			SpeedLimit: u.SpeedLimit,
			IPLimit:    u.IPLimit,
		})
	}
	inboundInfo.SubscriptionInfo = subscriptionMap
	l.InboundInfo.Store(tag, inboundInfo)
	return nil
}

func (l *Limiter) UpdateInboundLimiter(tag string, updatedServiceList *[]api.SubscriptionInfo) error {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return fmt.Errorf("no limiter found for tag %s", tag)
	}
	inboundInfo := value.(*InboundInfo)

	if inboundInfo.GlobalIPLimit.config == nil || !inboundInfo.GlobalIPLimit.config.Enable {
		log.Printf("[Limiter] Redis disabled for tag %s — IP limiting unavailable", tag)
	}

	for _, u := range *updatedServiceList {
		key := fmt.Sprintf("%s_%s", tag, u.Email)
		inboundInfo.SubscriptionInfo.Store(key, SubscriptionInfo{
			Id:         u.Id,
			SpeedLimit: u.SpeedLimit,
			IPLimit:    u.IPLimit,
		})

		limit := determineRate(inboundInfo.NodeSpeedLimit, u.SpeedLimit)
		if limit > 0 {
			if bucket, ok := inboundInfo.BucketHub.Load(key); ok {
				lim := bucket.(*rate.Limiter)
				lim.SetLimit(rate.Limit(limit))
				lim.SetBurst(int(limit))
			}
		} else {
			inboundInfo.BucketHub.Delete(key)
		}
	}
	return nil
}

// UpdateNodeInfo refreshes node-level limiter settings (speed limit and
// ignore-IP list) in-place, without recreating subscription state or Redis
// wiring. Used when the node info changes but the inbound tag stays the same.
func (l *Limiter) UpdateNodeInfo(tag string, nodeSpeedLimit uint64, ignoreIPs []string) error {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return fmt.Errorf("no limiter found for tag %s", tag)
	}
	inboundInfo := value.(*InboundInfo)
	inboundInfo.NodeSpeedLimit = nodeSpeedLimit
	inboundInfo.IgnoreIPs = ignoreIPs
	return nil
}

func (l *Limiter) DeleteInboundLimiter(tag string) error {
	l.InboundInfo.Delete(tag)
	return nil
}

func (l *Limiter) DeleteSubscriptionBuckets(tag string, emails []string) {
	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		for _, email := range emails {
			inboundInfo.BucketHub.Delete(email)
			inboundInfo.SubscriptionInfo.Delete(email)
		}
	}
}

func (l *Limiter) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return nil, fmt.Errorf("no such limiter: %s found", tag)
	}

	var onlineIP []api.OnlineIP
	inboundInfo := value.(*InboundInfo)

	if inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
		client := inboundInfo.GlobalIPLimit.client
		suffix := "|" + tag

		// Snapshot the subscriptions before touching Redis, so the sweep below
		// is not holding a sync.Map iteration open across every round trip.
		type target struct{ email, key string }
		var targets []target
		inboundInfo.SubscriptionInfo.Range(func(key, value interface{}) bool {
			email := key.(string)
			targets = append(targets, target{email, ipKey(strings.TrimPrefix(email, inboundInfo.Tag+"_"))})
			return true
		})

		// Subscriptions Redis knows nothing about, learned from the sweep
		// itself rather than from a second EXISTS per bucket.
		untracked := make(map[string]bool, len(targets))

		for start := 0; start < len(targets); start += redisBatchSize {
			batch := targets[start:min(start+redisBatchSize, len(targets))]

			// One deadline per batch, not one for the whole sweep. A single
			// budget spanning every subscription expires partway through on a
			// busy node, and each one after that fails for no reason of its own.
			ctx, cancel := context.WithTimeout(context.Background(), inboundInfo.GlobalIPLimit.config.timeout())

			reads := make([]*redis.MapStringStringCmd, len(batch))
			if _, err := client.Pipelined(ctx, func(p redis.Pipeliner) error {
				for i, t := range batch {
					reads[i] = p.HGetAll(ctx, t.key)
				}
				return nil
			}); err != nil {
				log.Printf("[Limiter] failed to read online IPs: %v", err)
			}

			deletes := make(map[string][]string, len(batch))
			for i, t := range batch {
				fields, err := reads[i].Result()
				if err != nil {
					// Leave the bucket alone: a failed read does not mean the
					// subscription has gone quiet.
					continue
				}
				if len(fields) == 0 {
					untracked[t.email] = true
					continue
				}
				claimed := make([]string, 0, len(fields))
				for field, uid := range fields {
					if !strings.HasSuffix(field, suffix) {
						continue // another node's entry for the same subscription
					}
					id, convErr := strconv.Atoi(uid)
					if convErr != nil {
						continue
					}
					onlineIP = append(onlineIP, api.OnlineIP{Id: id, IP: strings.TrimSuffix(field, suffix)})
					claimed = append(claimed, field)
				}
				if len(claimed) > 0 {
					deletes[t.key] = claimed
				}
			}

			// Only the fields just read are deleted, so an address recorded
			// between the read and the delete stays to be reported next cycle
			// rather than being dropped unreported.
			if len(deletes) > 0 {
				if _, err := client.Pipelined(ctx, func(p redis.Pipeliner) error {
					for key, fields := range deletes {
						p.HDel(ctx, key, fields...)
					}
					return nil
				}); err != nil {
					log.Printf("[Limiter] failed to clear reported IPs: %v", err)
				}
			}
			cancel()
		}

		// Drop rate buckets for subscriptions Redis is no longer tracking.
		inboundInfo.BucketHub.Range(func(key, value interface{}) bool {
			email := key.(string)
			if _, ok := inboundInfo.SubscriptionInfo.Load(email); !ok {
				return true
			}
			if untracked[email] {
				inboundInfo.BucketHub.Delete(email)
			}
			return true
		})
	}

	return &onlineIP, nil
}

func (l *Limiter) GetLimiter(tag string, email string, ip string) (limiter *rate.Limiter, isSpeedLimited bool, Reject bool) {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		log.Printf("[Limiter] no info for tag: %s", tag)
		return nil, false, false
	}

	var SpeedLimit uint64
	var ipLimit, uid int

	inboundInfo := value.(*InboundInfo)

	if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
		u := v.(SubscriptionInfo)
		uid = u.Id
		SpeedLimit = u.SpeedLimit
		ipLimit = u.IPLimit
	}

	ignored := false
	for _, ignoreip := range inboundInfo.IgnoreIPs {
		if ignoreip == ip {
			ignored = true
			break
		}
	}

	if !ignored && inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
		if checkLimit(inboundInfo, email, uid, ip, ipLimit, tag) {
			return nil, false, true
		}
	}

	limit := determineRate(inboundInfo.NodeSpeedLimit, SpeedLimit)
	if limit > 0 {
		lim := rate.NewLimiter(rate.Limit(limit), int(limit))
		if v, loaded := inboundInfo.BucketHub.LoadOrStore(email, lim); loaded {
			return v.(*rate.Limiter), true, false
		}
		return lim, true, false
	}
	return nil, false, false
}

type pendingCounter struct {
	up      stats.Counter
	down    stats.Counter
	upVal   int64
	downVal int64
}

type PendingTraffic struct {
	Result   []api.SubscriptionTraffic
	Counters []pendingCounter
}

// Chunk splits pending into batches of at most size records.
//
// Each batch keeps the counters belonging to its own records, so it can be
// reported and reset independently: one batch failing must neither discard
// counters another batch delivered nor retain ones it did not.
func (p *PendingTraffic) Chunk(size int) []*PendingTraffic {
	if p == nil || len(p.Result) == 0 {
		return nil
	}
	if size <= 0 || len(p.Result) <= size {
		return []*PendingTraffic{p}
	}
	chunks := make([]*PendingTraffic, 0, (len(p.Result)+size-1)/size)
	for start := 0; start < len(p.Result); start += size {
		end := min(start+size, len(p.Result))
		chunks = append(chunks, &PendingTraffic{
			Result:   p.Result[start:end],
			Counters: p.Counters[start:end],
		})
	}
	return chunks
}

func (l *Limiter) DrainDeltas(tag string) *PendingTraffic {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return nil
	}
	inboundInfo := value.(*InboundInfo)

	pending := &PendingTraffic{}

	inboundInfo.SubscriptionInfo.Range(func(k, v interface{}) bool {
		email := k.(string)
		sub := v.(SubscriptionInfo)

		upCounter := l.stm.GetCounter("user>>>" + email + ">>>traffic>>>uplink")
		downCounter := l.stm.GetCounter("user>>>" + email + ">>>traffic>>>downlink")

		var up, down int64
		if upCounter != nil {
			up = upCounter.Value()
		}
		if downCounter != nil {
			down = downCounter.Value()
		}
		if up == 0 && down == 0 {
			return true
		}

		pending.Result = append(pending.Result, api.SubscriptionTraffic{
			Id:       sub.Id,
			Upload:   up,
			Download: down,
		})

		pending.Counters = append(pending.Counters, pendingCounter{
			up: upCounter, down: downCounter, upVal: up, downVal: down,
		})
		return true
	})

	if len(pending.Result) == 0 {
		return nil
	}
	return pending
}

func (l *Limiter) ResetTraffic(pending *PendingTraffic) {
	if pending == nil {
		return
	}
	for _, pc := range pending.Counters {
		if pc.up != nil {
			pc.up.Add(-pc.upVal)
		}
		if pc.down != nil {
			pc.down.Add(-pc.downVal)
		}
	}
}

func checkLimit(inboundInfo *InboundInfo, email string, uid int, ip string, ipLimit int, tag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), inboundInfo.GlobalIPLimit.config.timeout())
	defer cancel()

	key := ipKey(strings.TrimPrefix(email, inboundInfo.Tag+"_"))
	ttl := int(inboundInfo.GlobalIPLimit.expiry / time.Second)
	if ttl <= 0 {
		ttl = 1
	}

	rejected, err := ipLimitScript.Run(ctx, inboundInfo.GlobalIPLimit.client,
		[]string{key}, ipField(ip, tag), uid, ipLimit, ttl, ip).Int()
	if err != nil {
		// Fail open. A Redis problem locking every user out of every node is a
		// far worse outcome than briefly not enforcing the address limit.
		log.Printf("[Limiter] IP limit check failed for %s: %v", key, err)
		return false
	}
	return rejected == 1
}

func determineRate(nodeLimit, subscriptionLimit uint64) uint64 {
	switch {
	case nodeLimit == 0 && subscriptionLimit == 0:
		return 0
	case nodeLimit == 0:
		return subscriptionLimit
	case subscriptionLimit == 0:
		return nodeLimit
	default:
		if nodeLimit < subscriptionLimit {
			return nodeLimit
		}
		return subscriptionLimit
	}
}
