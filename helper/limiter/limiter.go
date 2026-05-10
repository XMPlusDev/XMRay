// Package limiter is to control the links that go into the dispatcher
package limiter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"log"

	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/marshaler"
	"github.com/eko/gocache/lib/v4/store"
	redisStore "github.com/eko/gocache/store/redis/v4"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/xmplusdev/xmray/api"
)

type SubscriptionInfo struct {
	Id           int
	SpeedLimit   uint64
	IPLimit      int
	TrafficLimit int64 
	UsedTraffic  int64 
}

type IPData struct {
	UID   int
	Tag   string
	Email string
}

type InboundInfo struct {
	Tag            string
	NodeSpeedLimit uint64
	SubscriptionInfo *sync.Map // key: email → SubscriptionInfo
	BucketHub        *sync.Map // key: email → *rate.Limiter
	TrafficUp   *sync.Map // key: email → *atomic.Int64
	TrafficDown *sync.Map // key: email → *atomic.Int64
	GlobalIPLimit struct {
		config         *RedisConfig
		globalOnlineIP *marshaler.Marshaler
		redisClient    *redis.Client
	}
}

type Limiter struct {
	InboundInfo *sync.Map // key: tag → *InboundInfo
}

func New() *Limiter {
	return &Limiter{
		InboundInfo: new(sync.Map),
	}
}

func (l *Limiter) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, serviceList *[]api.SubscriptionInfo, redisConfig *RedisConfig) error {
	inboundInfo := &InboundInfo{
		Tag:            tag,
		NodeSpeedLimit: nodeSpeedLimit,
		BucketHub:      new(sync.Map),
		TrafficUp:      new(sync.Map), 
		TrafficDown:    new(sync.Map),
	}

	if redisConfig != nil && redisConfig.Enable {
		inboundInfo.GlobalIPLimit.config = redisConfig
		rc := redis.NewClient(&redis.Options{
			Network:  redisConfig.Network,
			Addr:     redisConfig.Addr,
			Username: redisConfig.Username,
			Password: redisConfig.Password,
			DB:       redisConfig.DB,
		})
		inboundInfo.GlobalIPLimit.redisClient = rc
		rs := redisStore.NewRedis(rc, store.WithExpiration(time.Duration(expiry)*time.Second))
		inboundInfo.GlobalIPLimit.globalOnlineIP = marshaler.New(cache.New[any](rs))
	}

	serviceMap := new(sync.Map)
	for _, u := range *serviceList {
		serviceMap.Store(fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id), SubscriptionInfo{
			Id:           u.Id,
			SpeedLimit:   u.SpeedLimit,
			IPLimit:      u.IPLimit,
			TrafficLimit: u.TrafficLimit, 
			UsedTraffic:  u.UsedTraffic, 
		})
	}
	inboundInfo.SubscriptionInfo = serviceMap
	l.InboundInfo.Store(tag, inboundInfo)
	return nil
}

func (l *Limiter) UpdateInboundLimiter(tag string, updatedServiceList *[]api.SubscriptionInfo) error {
	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		for _, u := range *updatedServiceList {
			key := fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id)
			inboundInfo.SubscriptionInfo.Store(key, SubscriptionInfo{
				Id:           u.Id,
				SpeedLimit:   u.SpeedLimit,
				IPLimit:      u.IPLimit,
				TrafficLimit: u.TrafficLimit, 
				UsedTraffic:  u.UsedTraffic, 
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
	} else {
		return fmt.Errorf("No such limiter: %s found", tag)
	}
	return nil
}

func (l *Limiter) DeleteInboundLimiter(tag string) error {
	if v, ok := l.InboundInfo.Load(tag); ok {
		info := v.(*InboundInfo)
		if info.GlobalIPLimit.redisClient != nil {
			if err := info.GlobalIPLimit.redisClient.Close(); err != nil {
				log.Printf("error closing Redis client for tag %s: %v", tag, err)
			}
		}
	}
	l.InboundInfo.Delete(tag)
	return nil
}

func (l *Limiter) DeleteSubscriptionBuckets(tag string, emails []string) {
	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		for _, email := range emails {
			inboundInfo.BucketHub.Delete(email)
		}
	}
}

func (l *Limiter) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	var onlineIP []api.OnlineIP

	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)

		if inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
			defer cancel()

			inboundInfo.BucketHub.Range(func(key, value interface{}) bool {
				email := key.(string)
				if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
					subscriptionInfo := v.(SubscriptionInfo)
					uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(subscriptionInfo.IPLimit), 1)
					v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
					if err != nil {
						inboundInfo.BucketHub.Delete(email)
					} else {
						ipMap := v.(*map[string][]IPData)
						emailFound := false
						for _, dataList := range *ipMap {
							for _, data := range dataList {
								if data.Email == email {
									emailFound = true
									break
								}
							}
							if emailFound {
								break
							}
						}
						if !emailFound {
							inboundInfo.BucketHub.Delete(email)
						}
					}
				}
				return true
			})

			inboundInfo.SubscriptionInfo.Range(func(key, value interface{}) bool {
				email := key.(string)
				subscriptionInfo := value.(SubscriptionInfo)
				uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(subscriptionInfo.IPLimit), 1)
				v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
				if err == nil {
					ipMap := v.(*map[string][]IPData)
					modified := false
					for ip, dataList := range *ipMap {
						remaining := dataList[:0]
						for _, data := range dataList {
							if data.Tag == tag {
								onlineIP = append(onlineIP, api.OnlineIP{Id: data.UID, IP: ip})
								modified = true
							} else {
								remaining = append(remaining, data)
							}
						}
						(*ipMap)[ip] = remaining
					}
					if modified {
						go pushIP(inboundInfo, uniqueKey, ipMap)
					}
				}
				return true
			})
		}
	} else {
		return nil, fmt.Errorf("No such limiter: %s found", tag)
	}

	return &onlineIP, nil
}

func (l *Limiter) GetLimiter(tag string, email string, ip string) (limiter *rate.Limiter, isSpeedLimited bool, Reject bool) {
	if value, ok := l.InboundInfo.Load(tag); ok {
		var (
			SpeedLimit   uint64
			ipLimit, uid int
			trafficLimit int64
			usedTraffic  int64
		)

		inboundInfo := value.(*InboundInfo)
		nodeLimit := inboundInfo.NodeSpeedLimit

		if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
			u := v.(SubscriptionInfo)
			uid = u.Id
			SpeedLimit = u.SpeedLimit
			ipLimit = u.IPLimit
			trafficLimit = u.TrafficLimit
			usedTraffic = u.UsedTraffic 
		}

		if trafficLimit > 0 {
			upRaw, _ := inboundInfo.TrafficUp.LoadOrStore(email, new(atomic.Int64))
			downRaw, _ := inboundInfo.TrafficDown.LoadOrStore(email, new(atomic.Int64))
			liveDelta := upRaw.(*atomic.Int64).Load() + downRaw.(*atomic.Int64).Load()
			if usedTraffic+liveDelta >= trafficLimit {
				return nil, false, true 
			}
		}

		if inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
			if reject := checkLimit(inboundInfo, email, uid, ip, ipLimit, tag); reject {
				return nil, false, true
			}
		}

		limit := determineRate(nodeLimit, SpeedLimit)
		if limit == 0 {
			return nil, false, false
		}
		if v, ok := inboundInfo.BucketHub.Load(email); ok {
			return v.(*rate.Limiter), true, false
		}
		lim := rate.NewLimiter(rate.Limit(limit), int(limit))
		if v, loaded := inboundInfo.BucketHub.LoadOrStore(email, lim); loaded {
			return v.(*rate.Limiter), true, false
		}
		return lim, true, false
	}
	newError("Get Limiter information failed").AtDebug()
	return nil, false, false
}

func (l *Limiter) AddDelta(tag, email string, upload, download int64) (exceeded bool) {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return false
	}
	inboundInfo := value.(*InboundInfo)

	subRaw, ok := inboundInfo.SubscriptionInfo.Load(email)
	if !ok {
		return false
	}
	sub := subRaw.(SubscriptionInfo)
	if sub.TrafficLimit == 0 {
		if upload > 0 {
			upRaw, _ := inboundInfo.TrafficUp.LoadOrStore(email, new(atomic.Int64))
			upRaw.(*atomic.Int64).Add(upload)
		}
		if download > 0 {
			downRaw, _ := inboundInfo.TrafficDown.LoadOrStore(email, new(atomic.Int64))
			downRaw.(*atomic.Int64).Add(download)
		}
		return false
	}

	upRaw, _ := inboundInfo.TrafficUp.LoadOrStore(email, new(atomic.Int64))
	downRaw, _ := inboundInfo.TrafficDown.LoadOrStore(email, new(atomic.Int64))
	newUp := upRaw.(*atomic.Int64).Add(upload)
	newDown := downRaw.(*atomic.Int64).Add(download)

	return sub.UsedTraffic+newUp+newDown >= sub.TrafficLimit
}

func (l *Limiter) DrainDeltas(tag string) []api.SubscriptionTraffic {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return nil
	}
	inboundInfo := value.(*InboundInfo)

	var result []api.SubscriptionTraffic
	inboundInfo.TrafficUp.Range(func(k, v interface{}) bool {
		email := k.(string)
		up := v.(*atomic.Int64).Swap(0)

		var down int64
		if downRaw, ok := inboundInfo.TrafficDown.Load(email); ok {
			down = downRaw.(*atomic.Int64).Swap(0)
		}
		if up == 0 && down == 0 {
			return true
		}
		subRaw, ok := inboundInfo.SubscriptionInfo.Load(email)
		if !ok {
			return true
		}
		sub := subRaw.(SubscriptionInfo)
		result = append(result, api.SubscriptionTraffic{
			Id:       sub.Id,
			Upload:   up,
			Download: down,
		})
		return true
	})
	return result
}

func (l *Limiter) CheckTrafficExceeded(tag string) []string {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return nil
	}
	inboundInfo := value.(*InboundInfo)

	var exceeded []string
	inboundInfo.SubscriptionInfo.Range(func(k, v interface{}) bool {
		email := k.(string)
		sub := v.(SubscriptionInfo)
		if sub.TrafficLimit == 0 {
			return true
		}
		var up, down int64
		if upRaw, ok := inboundInfo.TrafficUp.Load(email); ok {
			up = upRaw.(*atomic.Int64).Load()
		}
		if downRaw, ok := inboundInfo.TrafficDown.Load(email); ok {
			down = downRaw.(*atomic.Int64).Load()
		}
		if sub.UsedTraffic+up+down >= sub.TrafficLimit {
			exceeded = append(exceeded, email)
		}
		return true
	})
	return exceeded
}

func checkLimit(inboundInfo *InboundInfo, email string, uid int, ip string, ipLimit int, tag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
	defer cancel()

	uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(ipLimit), 1)
	v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
	if err != nil {
		if _, ok := err.(*store.NotFound); ok {
			go pushIP(inboundInfo, uniqueKey, &map[string][]IPData{ip: {{UID: uid, Tag: tag, Email: email}}})
		} else {
			newError("cache service").Base(err).AtError()
		}
		return false
	}

	ipMap := v.(*map[string][]IPData)
	if dataList, ipExists := (*ipMap)[ip]; ipExists {
		found := false
		for i, data := range dataList {
			if data.UID == uid && data.Tag == tag {
				dataList[i] = IPData{UID: uid, Tag: tag, Email: email}
				found = true
				break
			}
		}
		if !found {
			dataList = append(dataList, IPData{UID: uid, Tag: tag, Email: email})
		}
		(*ipMap)[ip] = dataList
		go pushIP(inboundInfo, uniqueKey, ipMap)
		return false
	}

	if ipLimit > 0 && len(*ipMap) >= ipLimit {
		return true
	}

	(*ipMap)[ip] = []IPData{{UID: uid, Tag: tag, Email: email}}
	go pushIP(inboundInfo, uniqueKey, ipMap)
	return false
}

func pushIP(inboundInfo *InboundInfo, uniqueKey string, ipMap *map[string][]IPData) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
	defer cancel()
	if err := inboundInfo.GlobalIPLimit.globalOnlineIP.Set(ctx, uniqueKey, ipMap); err != nil {
		newError("Redis cache service").Base(err).AtError()
	}
}

func determineRate(nodeLimit, subscriptionLimit uint64) (limit uint64) {
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