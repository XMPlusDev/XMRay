// Package limiter is to control the links that go into the dispatcher
package limiter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eko/gocache/lib/v4/cache"
	"github.com/eko/gocache/lib/v4/marshaler"
	"github.com/eko/gocache/lib/v4/store"
	redisStore "github.com/eko/gocache/store/redis/v4"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
	
	"github.com/xmplusdev/xmplus-server/api"
)

type SubscriptionInfo struct {
	Id          int
	SpeedLimit  uint64
	IPLimit     int
}

type IPData struct {
	UID   int
	Tag   string
	Email string
}

type InboundInfo struct {
	Tag            		   string
	NodeSpeedLimit 		   uint64
	SubscriptionInfo   	   *sync.Map // Key: Email value: SubscriptionInfo
	BucketHub      		   *sync.Map // key: Email, value: *rate.Limiter
	GlobalIPLimit  struct {
		config         *RedisConfig
		globalOnlineIP *marshaler.Marshaler
	}
}

type Limiter struct {
	InboundInfo *sync.Map // Key: Tag, Value: *InboundInfo
}

func New() *Limiter {
	return &Limiter{
		InboundInfo: new(sync.Map),
	}
}

func (l *Limiter) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, serviceList *[]api.SubscriptionInfo, redisConfig *RedisConfig) error {
	inboundInfo := &InboundInfo{
		Tag:            		tag,
		NodeSpeedLimit: 		nodeSpeedLimit,
		BucketHub:      		new(sync.Map),
	}

	if redisConfig != nil && redisConfig.Enable {
		inboundInfo.GlobalIPLimit.config = redisConfig

		rs := redisStore.NewRedis(redis.NewClient(
			&redis.Options{
				Network:  redisConfig.Network,
				Addr:     redisConfig.Addr,
				Username: redisConfig.Username,
				Password: redisConfig.Password,
				DB:       redisConfig.DB,
			}),
			store.WithExpiration(time.Duration(expiry)*time.Second))
		
		cacheManager := cache.New[any](rs)
		inboundInfo.GlobalIPLimit.globalOnlineIP = marshaler.New(cacheManager)
	}
	
	serviceMap := new(sync.Map)
	for _, u := range *serviceList {
		serviceMap.Store(fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id), SubscriptionInfo{
			Id:          u.Id,
			SpeedLimit:  u.SpeedLimit,
			IPLimit:     u.IPLimit,
		})
	}
	inboundInfo.SubscriptionInfo = serviceMap
	l.InboundInfo.Store(tag, inboundInfo) // Replace the old inbound info
	return nil
}

func (l *Limiter) UpdateInboundLimiter(tag string, updatedServiceList *[]api.SubscriptionInfo) error {
	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		// Update User info
		for _, u := range *updatedServiceList {
			inboundInfo.SubscriptionInfo.Store(fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id), SubscriptionInfo{
				Id:          u.Id,
				SpeedLimit:  u.SpeedLimit,
				IPLimit: 	 u.IPLimit,
			})
			// Update old limiter bucket
			limit := determineRate(inboundInfo.NodeSpeedLimit, u.SpeedLimit)
			if limit > 0 {
				if bucket, ok := inboundInfo.BucketHub.Load(fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id)); ok {
					limiter := bucket.(*rate.Limiter)
					limiter.SetLimit(rate.Limit(limit))
					limiter.SetBurst(int(limit))
				}
			} else {
				inboundInfo.BucketHub.Delete(fmt.Sprintf("%s|%s|%d", tag, u.Email, u.Id))
			}
		}
	} else {
		return fmt.Errorf("No such limiter: %s found", tag)
	}
	return nil
}

func (l *Limiter) DeleteInboundLimiter(tag string) error {
	l.InboundInfo.Delete(tag)
	return nil
}

func (l *Limiter) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	var onlineIP []api.OnlineIP

	if value, ok := l.InboundInfo.Load(tag); ok {
		inboundInfo := value.(*InboundInfo)
		
		// If GlobalIPLimit is enabled, use Redis cache
		if inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
			defer cancel()
			
			// Clear Speed Limiter bucket for users who are not online (check Redis)
			inboundInfo.BucketHub.Range(func(key, value interface{}) bool {
				email := key.(string)
				
				if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
					subscriptionInfo := v.(SubscriptionInfo)
					uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(subscriptionInfo.IPLimit), 1)
					
					// Check if user exists in Redis
					v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
					if err != nil {
						// User not in Redis, delete bucket
						inboundInfo.BucketHub.Delete(email)
					} else {
						// User exists in Redis - check if this specific email exists in any IPData
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
						
						// If email not found in any IPData, delete bucket
						if !emailFound {
							inboundInfo.BucketHub.Delete(email)
						}
					}
				}
				return true
			})
			
			// Get all users from SubscriptionInfo to check their IPs in Redis
			inboundInfo.SubscriptionInfo.Range(func(key, value interface{}) bool {
				email := key.(string)
				subscriptionInfo := value.(SubscriptionInfo)
				
				// Reformat email for unique key (same as in globalLimit function)
				uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(subscriptionInfo.IPLimit), 1)
				
				// Get IP map from Redis
				v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
				if err == nil {
					ipMap := v.(*map[string][]IPData)
					modified := false

					for ip, dataList := range *ipMap {
						// Iterate through all IPData entries for this IP
						remaining := dataList[:0]
						for _, data := range dataList {
							if data.Tag == tag {
								onlineIP = append(onlineIP, api.OnlineIP{Id: data.UID, IP: ip})
								modified = true
							} else {
								remaining = append(remaining, data)
							}
						}

						if len(remaining) == 0 {
							(*ipMap)[ip] = []IPData{}
						} else {
							(*ipMap)[ip] = remaining
						}
					}

					// Push updated map back to Redis if we removed any entries
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

func (l *Limiter) GetLimiter(tag string, email string, ip string, address string) (limiter *rate.Limiter, isSpeedLimited bool, Reject bool) {
	if value, ok := l.InboundInfo.Load(tag); ok {
		var (
			SpeedLimit  uint64 = 0
			ipLimit, uid int
		)
		
		if ip == "" {
			ip = address
		}

		inboundInfo := value.(*InboundInfo)
		nodeLimit := inboundInfo.NodeSpeedLimit

		if v, ok := inboundInfo.SubscriptionInfo.Load(email); ok {
			u := v.(SubscriptionInfo)
			uid = u.Id
			SpeedLimit = u.SpeedLimit
			ipLimit = u.IPLimit
		}

		// Check IP limit based on whether GlobalIPLimit (Redis) is enabled
		if inboundInfo.GlobalIPLimit.config != nil && inboundInfo.GlobalIPLimit.config.Enable {
			// Use Redis for IP limit checking
			if reject := checkLimit(inboundInfo, email, uid, ip, ipLimit, tag); reject {
				return nil, false, true
			}
		}
		
		// Speed limit
		limit := determineRate(nodeLimit, SpeedLimit) // Determine the speed limit rate
		if limit > 0 {
			limiter := rate.NewLimiter(rate.Limit(limit), int(limit)) // Byte/s
			if v, ok := inboundInfo.BucketHub.LoadOrStore(email, limiter); ok {
				bucket := v.(*rate.Limiter)
				return bucket, true, false
			} else {
				return limiter, true, false
			}
		} else {
			return nil, false, false
		}
	} else {
		newError("Get Limiter information failed").AtDebug()
		return nil, false, false
	}
}

// Global device limit
func checkLimit(inboundInfo *InboundInfo, email string, uid int, ip string, ipLimit int, tag string) bool {
    ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
    defer cancel()

    // reformat email for unique key
    uniqueKey := strings.Replace(email, inboundInfo.Tag, strconv.Itoa(ipLimit), 1)

    v, err := inboundInfo.GlobalIPLimit.globalOnlineIP.Get(ctx, uniqueKey, new(map[string][]IPData))
    if err != nil {
        if _, ok := err.(*store.NotFound); ok {
            // If the subscription email is a new device (first connection)
            go pushIP(inboundInfo, uniqueKey, &map[string][]IPData{ip: {{UID: uid, Tag: tag, Email: email}}})
        } else {
            newError("cache service").Base(err).AtError()
        }
        return false
    }

    ipMap := v.(*map[string][]IPData)
	
	// Check if this IP already exists in cache
	if dataList, ipExists := (*ipMap)[ip]; ipExists {
		// IP exists - check if this UID/Tag combination exists
		found := false
		for i, data := range dataList {
			if data.UID == uid && data.Tag == tag {
				// Update existing entry
				dataList[i] = IPData{UID: uid, Tag: tag, Email: email}
				found = true
				break
			}
		}
		
		// If UID or Tag is different, append new IPData
		if !found {
			dataList = append(dataList, IPData{UID: uid, Tag: tag, Email: email})
		}
		
		(*ipMap)[ip] = dataList
		go pushIP(inboundInfo, uniqueKey, ipMap)
		return false
	}
    
    // This is a NEW IP - check if we're at limit
    if ipLimit > 0 && len(*ipMap) >= ipLimit {
        // Already at limit, reject the NEW IP
        return true
    }

    // Within limit, add the new IP with IPData as a slice
    (*ipMap)[ip] = []IPData{{UID: uid, Tag: tag, Email: email}}
    go pushIP(inboundInfo, uniqueKey, ipMap)

    return false
}

// push the ip to cache
func pushIP(inboundInfo *InboundInfo, uniqueKey string, ipMap *map[string][]IPData) {
    ctx, cancel := context.WithTimeout(context.Background(), time.Duration(inboundInfo.GlobalIPLimit.config.Timeout)*time.Second)
    defer cancel()

    if err := inboundInfo.GlobalIPLimit.globalOnlineIP.Set(ctx, uniqueKey, ipMap); err != nil {
        newError("Redis cache service").Base(err).AtError()
    }
}

// determineRate returns the minimum non-zero rate
func determineRate(nodeLimit, SubscriptionLimit uint64) (limit uint64) {
	if nodeLimit <= 0 && SubscriptionLimit <= 0 {
		return 0
	} else {
		if nodeLimit < 0 {
			nodeLimit = 0 
		}
		if SubscriptionLimit < 0 {
			SubscriptionLimit = 0 
		}
		if nodeLimit == 0 && SubscriptionLimit > 0 {
			return SubscriptionLimit
		} else if nodeLimit > 0 && SubscriptionLimit == 0 {
			return nodeLimit
		} else if nodeLimit > SubscriptionLimit {
			return SubscriptionLimit
		} else if nodeLimit < SubscriptionLimit {
			return nodeLimit
		} else {
			return nodeLimit
		}
	}
}