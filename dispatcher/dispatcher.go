package dispatcher

import (
	"context"
	"fmt"
	logger "log"
	netModule "net"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/transport"
	"golang.org/x/time/rate"

	"github.com/xmplusdev/xmray/api"
	"github.com/xmplusdev/xmray/limiter"
)

func maskIP(ipStr string, keepSegments int) string {
	ip := netModule.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	if ip.To4() != nil {
		parts := strings.Split(ipStr, ".")
		if len(parts) != 4 {
			return ipStr
		}
		for i := keepSegments; i < 4; i++ {
			parts[i] = "*"
		}
		return strings.Join(parts, ".")
	}
	fullIP := ip.String()
	parts := strings.Split(fullIP, ":")
	for i := keepSegments; i < len(parts); i++ {
		parts[i] = "*"
	}
	return strings.Join(parts, ":")
}

type sessionInfo struct {
	tag   string
	email string 
	ip    string
}

func RegisterOn(server *core.Instance, lim *limiter.Limiter) (*LimitingDispatcher, error) {
	raw := server.GetFeature(routing.DispatcherType())
	if raw == nil {
		return nil, errors.New("no dispatcher feature found in instance")
	}
	inner, ok := raw.(routing.Dispatcher)
	if !ok {
		return nil, errors.New("dispatcher feature does not implement routing.Dispatcher")
	}

	ibm, _ := server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	stm, _ := server.GetFeature(stats.ManagerType()).(stats.Manager)

	ld := &LimitingDispatcher{
		inner:   inner,
		limiter: lim,
		ibm:     ibm,
		stm:     stm,
	}

	if err := replaceFeature(server, routing.DispatcherType(), ld); err != nil {
		return nil, err
	}

	return ld, nil
}

func replaceFeature(server *core.Instance, targetType interface{}, ld *LimitingDispatcher) error {
	rv := reflect.ValueOf(server).Elem()
	featField := rv.FieldByName("features")
	if !featField.IsValid() {
		return fmt.Errorf("core.Instance has no 'features' field")
	}
	writable := reflect.NewAt(featField.Type(), unsafe.Pointer(featField.UnsafeAddr())).Elem()
	slice := writable.Interface().([]features.Feature)
	for i, f := range slice {
		if f.Type() == targetType {
			slice[i] = ld
			writable.Set(reflect.ValueOf(slice))
			return nil
		}
	}
	return fmt.Errorf("no feature with type %v found", targetType)
}

type LimitingDispatcher struct {
	inner   routing.Dispatcher
	limiter *limiter.Limiter
	ibm     inbound.Manager
	stm     stats.Manager

	connsMu sync.Mutex
	conns   map[string]map[netModule.Conn]struct{}
	
	linksMu sync.Mutex
	links map[string]map[*transport.Link]struct{}
}

func (ld *LimitingDispatcher) Type() interface{} { return routing.DispatcherType() }

func (ld *LimitingDispatcher) Start() error {
	if s, ok := ld.inner.(common.Runnable); ok {
		return s.Start()
	}
	return nil
}

func (ld *LimitingDispatcher) Close() error { return common.Close(ld.inner) }

func (ld *LimitingDispatcher) isUserValidInInbound(ctx context.Context, user *protocol.MemoryUser, inboundTag string) bool {
	if user == nil || len(user.Email) == 0 {
		return false
	}
	if ld.ibm == nil {
		return true
	}
	handler, err := ld.ibm.GetHandler(ctx, inboundTag)
	if err != nil {
		return false
	}
	userManager, ok := handler.(proxy.UserManager)
	if !ok {
		return true
	}
	return userManager.GetUser(ctx, user.Email) != nil
}

func resolveUserLimits(sessionInbound *session.Inbound) *sessionInfo {
	if sessionInbound == nil || sessionInbound.User == nil {
		return nil
	}
	user := sessionInbound.User
	if len(user.Email) == 0 {
		return nil
	}
	return &sessionInfo{
		tag:   sessionInbound.Tag,
		email: user.Email,
		ip:    sessionInbound.Source.Address.IP().String(),
	}
}

type sessionContext struct {
	inbound   *session.Inbound
	info      *sessionInfo
	user      *protocol.MemoryUser
	bucket    *rate.Limiter
	hasBucket bool
	link      *transport.Link
}

func (ld *LimitingDispatcher) trackConn(key string, conn netModule.Conn) func() {
	if conn == nil || key == "" {
		return func() {}
	}

	ld.connsMu.Lock()
	if ld.conns == nil {
		ld.conns = make(map[string]map[netModule.Conn]struct{})
	}
	if ld.conns[key] == nil {
		ld.conns[key] = make(map[netModule.Conn]struct{})
	}
	ld.conns[key][conn] = struct{}{}
	ld.connsMu.Unlock()

	return func() {
		ld.connsMu.Lock()
		if set, ok := ld.conns[key]; ok {
			delete(set, conn)
			if len(set) == 0 {
				delete(ld.conns, key)
			}
		}
		ld.connsMu.Unlock()
	}
}


func (ld *LimitingDispatcher) resolveSession(ctx context.Context, link *transport.Link) (*sessionContext, error) {
	sessionInbound := session.InboundFromContext(ctx)
	if sessionInbound == nil {
		return nil, nil
	}

	sessionInbound.CanSpliceCopy = 3

	info := resolveUserLimits(sessionInbound)
	if info == nil {
		return nil, nil
	}

	user := sessionInbound.User

	if !ld.isUserValidInInbound(ctx, user, info.tag) {
		parts := strings.Split(info.email, "|")
		logger.Printf("Subscription (ID:%s) deleted. Closing connection", parts[len(parts)-1])
		if sessionInbound.Conn != nil {
			sessionInbound.Conn.Close()
		}
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return nil, errors.New("closing connection for: ", info.email)
	}

	bucket, isSpeedLimited, reject := ld.limiter.GetLimiter(info.tag, info.email, info.ip)

	if reject {
		parts := strings.Split(info.email, "|")
		logger.Printf("Subscription (ID:%s) ip/traffic limit exceeded. Connection from %s aborted",
			parts[len(parts)-1], maskIP(info.ip, 2))
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return nil, errors.New("subscription limit exceeded for: ", info.email)
	}

	if sessionInbound.Conn != nil {
		untrack := ld.trackConn(info.email, sessionInbound.Conn)
		context.AfterFunc(ctx, untrack)
	}
	
	untrackLink := ld.trackLink(info.email, link)
	context.AfterFunc(ctx, untrackLink)

	return &sessionContext{
		inbound:   sessionInbound,
		info:      info,
		user:      user,
		bucket:    bucket,
		hasBucket: isSpeedLimited && bucket != nil,
		link:      link,
	}, nil
}

func (ld *LimitingDispatcher) trackLink(key string, link *transport.Link) func() {
	if link == nil || key == "" {
		return func() {}
	}
	ld.linksMu.Lock()
	if ld.links == nil {
		ld.links = make(map[string]map[*transport.Link]struct{})
	}
	if ld.links[key] == nil {
		ld.links[key] = make(map[*transport.Link]struct{})
	}
	ld.links[key][link] = struct{}{}
	ld.linksMu.Unlock()

	return func() {
		ld.linksMu.Lock()
		if set, ok := ld.links[key]; ok {
			delete(set, link)
			if len(set) == 0 {
				delete(ld.links, key)
			}
		}
		ld.linksMu.Unlock()
	}
}

func (ld *LimitingDispatcher) getLink(ctx context.Context, link *transport.Link) error {
	sc, err := ld.resolveSession(ctx, link)
	if err != nil || sc == nil {
		return err
	}

	if sc.hasBucket {
		link.Writer = ld.limiter.RateWriter(link.Writer, sc.bucket)
	}

	return nil
}

func (ld *LimitingDispatcher) wrapLink(ctx context.Context, link *transport.Link) (*transport.Link, error) {
	twr := &buf.TimeoutWrapperReader{Reader: link.Reader}
	link.Reader = twr

	sc, err := ld.resolveSession(ctx, link)
	if err != nil || sc == nil {
		return link, err
	}

	if sc.hasBucket {
		link.Writer = ld.limiter.RateWriter(link.Writer, sc.bucket)
		link.Reader = ld.limiter.RateTimeoutReader(twr, sc.bucket)
	}

	return link, nil
}

func (ld *LimitingDispatcher) Dispatch(ctx context.Context, dest net.Destination) (*transport.Link, error) {
	link, err := ld.inner.Dispatch(ctx, dest)
	if err != nil {
		return nil, err
	}
	if err := ld.getLink(ctx, link); err != nil {
		return nil, err
	}
	return link, nil
}

func (ld *LimitingDispatcher) DispatchLink(ctx context.Context, dest net.Destination, link *transport.Link) error {
	wrapped, err := ld.wrapLink(ctx, link)
	if err != nil {
		return err
	}
	return ld.inner.DispatchLink(ctx, dest, wrapped)
}

func (ld *LimitingDispatcher) AddInboundLimiter(tag string, expiry int, nodeSpeedLimit uint64, ignoreIPs []string, subscriptionList *[]api.SubscriptionInfo) error {
	return ld.limiter.AddInboundLimiter(tag, expiry, nodeSpeedLimit, ignoreIPs, subscriptionList)
}

func (ld *LimitingDispatcher) UpdateInboundLimiter(tag string, updatedSubscriptionList *[]api.SubscriptionInfo) error {
	return ld.limiter.UpdateInboundLimiter(tag, updatedSubscriptionList)
}

func (ld *LimitingDispatcher) UpdateNodeInfo(tag string, nodeSpeedLimit uint64, ignoreIPs []string) error {
	return ld.limiter.UpdateNodeInfo(tag, nodeSpeedLimit, ignoreIPs)
}

func (ld *LimitingDispatcher) DeleteInboundLimiter(tag string) error {
	return ld.limiter.DeleteInboundLimiter(tag)
}

func (ld *LimitingDispatcher) DeleteSubscriptionBuckets(tag string, emails []string) {
	ld.limiter.DeleteSubscriptionBuckets(tag, emails)

	ld.connsMu.Lock()
	var conns []netModule.Conn
	for _, key := range emails {
		if set, ok := ld.conns[key]; ok {
			for c := range set {
				conns = append(conns, c)
			}
			delete(ld.conns, key)
		}
	}
	ld.connsMu.Unlock()

	ld.linksMu.Lock()
	var links []*transport.Link
	for _, key := range emails {
		if set, ok := ld.links[key]; ok {
			for l := range set {
				links = append(links, l)
			}
			delete(ld.links, key)
		}
	}
	ld.linksMu.Unlock()

	for _, c := range conns {
		c.Close()
	}
	for _, l := range links {
		common.Close(l.Writer)
		common.Interrupt(l.Reader)
	}
}

func (ld *LimitingDispatcher) GetOnlineIPs(tag string) (*[]api.OnlineIP, error) {
	return ld.limiter.GetOnlineIPs(tag)
}

func (ld *LimitingDispatcher) DrainDeltas(tag string) *limiter.PendingTraffic {
	return ld.limiter.DrainDeltas(tag)
}

func (ld *LimitingDispatcher) ResetTraffic(pending *limiter.PendingTraffic) {
	if pending == nil {
		return
	}
	ld.limiter.ResetTraffic(pending)
}