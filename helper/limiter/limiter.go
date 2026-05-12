// Package limiter is to control the links that go into the dispatcher
package limiter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
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

const (
	trafficUpPrefix   = "xmray:traffic:up:"
	trafficDownPrefix = "xmray:traffic:down:"
	trafficUsedPrefix  = "xmray:traffic:used:"
    trafficLimitPrefix = "xmray:traffic:limit:"
)

func trafficUpKey(email string) string   { return trafficUpPrefix + email }
func trafficDownKey(email string) string { return trafficDownPrefix + email }
func trafficUsedKey(uniqueKey string) string { return trafficUsedPrefix + uniqueKey }
func trafficLimitKey(uniqueKey string) string { return trafficLimitPrefix + uniqueKey }

type SubscriptionInfo struct {
	Id           int
	SpeedLimit   uint64
	IPLimit      int
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
	GlobalIPLimit struct {
		config         *RedisConfig
		globalOnlineIP *marshaler.Marshaler
		redisClient    *redis.Client
	}
	trafficRedis  *redis.Client
	trafficExpiry time.Duration 
}

type Limiter struct {
	InboundInfo *sync.Map // key: tag → *InboundInfo
}

func New() *Limiter {
	return &Limiter{InboundInfo: new(sync.Map)}
}

func (l *Limiter) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, subscriptionList *[]api.SubscriptionInfo, redisConfig *RedisConfig) error {
	inboundInfo := &InboundInfo{
		Tag:            tag,
		NodeSpeedLimit: nodeSpeedLimit,
		BucketHub:      new(sync.Map),
		trafficExpiry:  time.Duration(expiry*2) * time.Second,
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
		inboundInfo.trafficRedis = rc
	} else {
		return fmt.Errorf("[Limiter] : Redis config for 【NodeTAG=%s】 is disabled; traffic quota check, ip limit and node traffic report requires redis to be enabled", tag)
	}

	subscriptionMap := new(sync.Map)
	for _, u := range *subscriptionList {
		key := fmt.Sprintf("%s_%s", tag, u.Email)
		subscriptionMap.Store(key, SubscriptionInfo{
			Id:           u.Id,
			SpeedLimit:   u.SpeedLimit,
			IPLimit:      u.IPLimit,
		})
		
		if inboundInfo.trafficRedis != nil && u.TrafficLimit > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			uniqueKey := strings.TrimPrefix(key, tag+"_")
			inboundInfo.trafficRedis.SetNX(ctx, trafficUsedKey(uniqueKey),  u.UsedTraffic,  0)
			inboundInfo.trafficRedis.SetNX(ctx, trafficLimitKey(uniqueKey), u.TrafficLimit, 0)
			cancel()
		}
	}
	inboundInfo.SubscriptionInfo = subscriptionMap
	l.InboundInfo.Store(tag, inboundInfo)
	return nil
}

func (l *Limiter) UpdateInboundLimiter(tag string, updatedServiceList *[]api.SubscriptionInfo) error {
	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		for _, u := range *updatedServiceList {
			key := fmt.Sprintf("%s_%s", tag, u.Email)
			inboundInfo.SubscriptionInfo.Store(key, SubscriptionInfo{
				Id:           u.Id,
				SpeedLimit:   u.SpeedLimit,
				IPLimit:      u.IPLimit,
			})
			
			if inboundInfo.trafficRedis != nil && u.TrafficLimit > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				uniqueKey := strings.TrimPrefix(key, tag+"_")
				inboundInfo.trafficRedis.Set(ctx, trafficUsedKey(uniqueKey),  u.UsedTraffic,  0)
				inboundInfo.trafficRedis.Set(ctx, trafficLimitKey(uniqueKey), u.TrafficLimit, 0)
				cancel()
			}

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
		if info.trafficRedis != nil && info.trafficRedis != info.GlobalIPLimit.redisClient {
			info.trafficRedis.Close()
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
			inboundInfo.SubscriptionInfo.Delete(email)
			
			if inboundInfo.trafficRedis != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				uniqueKey := strings.TrimPrefix(email, tag+"_")
				inboundInfo.trafficRedis.Del(ctx,
					trafficUpKey(email),
					trafficDownKey(email),
					trafficUsedKey(uniqueKey),
					trafficLimitKey(uniqueKey),
				)
				cancel()
			}
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
				if _, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
					uniqueKey := strings.TrimPrefix(email, inboundInfo.Tag + "_")
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
				uniqueKey := strings.TrimPrefix(email, inboundInfo.Tag + "_")
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
		)

		inboundInfo := value.(*InboundInfo)
		nodeLimit := inboundInfo.NodeSpeedLimit

		if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
			u := v.(SubscriptionInfo)
			uid = u.Id
			SpeedLimit = u.SpeedLimit
			ipLimit = u.IPLimit
		}

		if inboundInfo.trafficRedis != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			uniqueKey := strings.TrimPrefix(email, tag+"_")

			pipe := inboundInfo.trafficRedis.Pipeline()
			usedCmd  := pipe.Get(ctx, trafficUsedKey(uniqueKey))
			limitCmd := pipe.Get(ctx, trafficLimitKey(uniqueKey))
			pipe.Exec(ctx)
			cancel()

			limitVal, err := limitCmd.Int64()
			if err == nil && limitVal > 0 {
				usedVal, err := usedCmd.Int64()
				if err == nil && usedVal >= limitVal {
					return nil, false, true 
				}
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

func (l *Limiter) AddDelta(tag, email string, upload, download int64) bool {
    value, ok := l.InboundInfo.Load(tag)
    if !ok {
        return false
    }
    inboundInfo := value.(*InboundInfo)
    if inboundInfo.trafficRedis == nil {
        return false
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    rc := inboundInfo.trafficRedis
    uniqueKey := strings.TrimPrefix(email, tag+"_")
    total := upload + download

    if upload > 0 {
        rc.IncrBy(ctx, trafficUpKey(email), upload)
    }
    if download > 0 {
        rc.IncrBy(ctx, trafficDownKey(email), download)
    }

    limitVal, err := rc.Get(ctx, trafficLimitKey(uniqueKey)).Int64()
    if err != nil || limitVal == 0 {
        return false
    }

    newUsed, err := rc.IncrBy(ctx, trafficUsedKey(uniqueKey), total).Result()
    if err != nil {
        return false
    }

    return newUsed >= limitVal
}

func (l *Limiter) DrainDeltas(tag string) []api.SubscriptionTraffic {
	value, ok := l.InboundInfo.Load(tag)
	if !ok {
		return nil
	}
	inboundInfo := value.(*InboundInfo)

	if inboundInfo.trafficRedis == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rc := inboundInfo.trafficRedis
	var result []api.SubscriptionTraffic

	inboundInfo.SubscriptionInfo.Range(func(k, v interface{}) bool {
		email := k.(string)
		sub := v.(SubscriptionInfo)

		upStr, errUp := rc.GetDel(ctx, trafficUpKey(email)).Result()
		downStr, errDown := rc.GetDel(ctx, trafficDownKey(email)).Result()

		var up, down int64
		if errUp == nil {
			up, _ = strconv.ParseInt(upStr, 10, 64)
		}
		if errDown == nil {
			down, _ = strconv.ParseInt(downStr, 10, 64)
		}

		if up == 0 && down == 0 {
			return true
		}
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
    if inboundInfo.trafficRedis == nil {
        return nil
    }

    var exceeded []string
    inboundInfo.SubscriptionInfo.Range(func(k, _ interface{}) bool {
        email := k.(string)
        uniqueKey := strings.TrimPrefix(email, tag+"_")

        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        pipe := inboundInfo.trafficRedis.Pipeline()
        usedCmd  := pipe.Get(ctx, trafficUsedKey(uniqueKey))
        limitCmd := pipe.Get(ctx, trafficLimitKey(uniqueKey))
        pipe.Exec(ctx)
        cancel()

        limitVal, err := limitCmd.Int64()
        if err != nil || limitVal == 0 {
            return true
        }
        usedVal, err := usedCmd.Int64()
        if err != nil {
            return true
        }
        if usedVal >= limitVal {
            exceeded = append(exceeded, email)
        }
        return true
    })
    return exceeded
}

func checkLimit(inboundInfo *InboundInfo, email string, uid int, ip string, ipLimit int, tag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
	defer cancel()

	uniqueKey := strings.TrimPrefix(email, inboundInfo.Tag + "_")
	
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
			(*ipMap)[ip] = append(dataList, IPData{UID: uid, Tag: tag, Email: email})
		} else {
			(*ipMap)[ip] = dataList
		}
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